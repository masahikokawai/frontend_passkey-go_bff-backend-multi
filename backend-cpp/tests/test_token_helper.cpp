#include "test_token_helper.hpp"

#include <openssl/bn.h>
#include <openssl/core_names.h>
#include <openssl/hmac.h>
#include <openssl/rsa.h>

#include <chrono>
#include <nlohmann/json.hpp>
#include <stdexcept>

#include "auth/base64.hpp"

namespace backend_cpp::testutil {

using json = nlohmann::json;

std::string Base64UrlEncode(const std::vector<uint8_t>& bytes) {
  static const char kAlphabet[] =
      "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
  std::string out;
  size_t i = 0;
  for (; i + 2 < bytes.size(); i += 3) {
    uint32_t v = (static_cast<uint32_t>(bytes[i]) << 16) | (static_cast<uint32_t>(bytes[i + 1]) << 8) |
                 bytes[i + 2];
    out.push_back(kAlphabet[(v >> 18) & 0x3F]);
    out.push_back(kAlphabet[(v >> 12) & 0x3F]);
    out.push_back(kAlphabet[(v >> 6) & 0x3F]);
    out.push_back(kAlphabet[v & 0x3F]);
  }
  size_t remaining = bytes.size() - i;
  if (remaining == 1) {
    uint32_t v = static_cast<uint32_t>(bytes[i]) << 16;
    out.push_back(kAlphabet[(v >> 18) & 0x3F]);
    out.push_back(kAlphabet[(v >> 12) & 0x3F]);
  } else if (remaining == 2) {
    uint32_t v = (static_cast<uint32_t>(bytes[i]) << 16) | (static_cast<uint32_t>(bytes[i + 1]) << 8);
    out.push_back(kAlphabet[(v >> 18) & 0x3F]);
    out.push_back(kAlphabet[(v >> 12) & 0x3F]);
    out.push_back(kAlphabet[(v >> 6) & 0x3F]);
  }
  // base64url(パディング無し)なので'='は付けない
  return out;
}

namespace {

std::string EncodeJson(const json& j) {
  std::string raw = j.dump();
  return Base64UrlEncode(std::vector<uint8_t>(raw.begin(), raw.end()));
}

int64_t NowUnix() {
  return std::chrono::duration_cast<std::chrono::seconds>(
             std::chrono::system_clock::now().time_since_epoch())
      .count();
}

}  // namespace

std::string MakeHmacToken(const std::string& secret, const std::string& iss,
                           const std::string& aud, const std::string& sub,
                           int64_t exp_offset_secs, const std::string& azp) {
  json header{{"alg", "HS256"}, {"typ", "JWT"}};
  json payload{{"sub", sub}, {"iss", iss}, {"aud", aud}, {"exp", NowUnix() + exp_offset_secs}};
  if (!azp.empty()) payload["azp"] = azp;

  std::string signing_input = EncodeJson(header) + "." + EncodeJson(payload);

  unsigned char mac[EVP_MAX_MD_SIZE];
  unsigned int mac_len = 0;
  HMAC(EVP_sha256(), secret.data(), static_cast<int>(secret.size()),
       reinterpret_cast<const unsigned char*>(signing_input.data()), signing_input.size(), mac,
       &mac_len);

  return signing_input + "." + Base64UrlEncode(std::vector<uint8_t>(mac, mac + mac_len));
}

EVP_PKEY* GenerateRsaKeypair() {
  EVP_PKEY* pkey = EVP_RSA_gen(2048);
  if (pkey == nullptr) throw std::runtime_error("EVP_RSA_gen failed");
  return pkey;
}

namespace {

std::vector<uint8_t> BignumParam(EVP_PKEY* pkey, const char* param_name) {
  BIGNUM* bn = nullptr;
  if (EVP_PKEY_get_bn_param(pkey, param_name, &bn) != 1 || bn == nullptr) {
    throw std::runtime_error(std::string("EVP_PKEY_get_bn_param failed: ") + param_name);
  }
  std::vector<uint8_t> out(static_cast<size_t>(BN_num_bytes(bn)));
  BN_bn2bin(bn, out.data());
  BN_free(bn);
  return out;
}

}  // namespace

RsaPublicComponents ExtractRsaPublicComponents(EVP_PKEY* pkey) {
  RsaPublicComponents out;
  out.n_b64url = Base64UrlEncode(BignumParam(pkey, OSSL_PKEY_PARAM_RSA_N));
  out.e_b64url = Base64UrlEncode(BignumParam(pkey, OSSL_PKEY_PARAM_RSA_E));
  return out;
}

std::string MakeRsaToken(EVP_PKEY* private_key, const std::string& kid, const std::string& iss,
                          const std::string& aud, const std::string& sub,
                          int64_t exp_offset_secs, const std::string& azp) {
  json header{{"alg", "RS256"}, {"typ", "JWT"}, {"kid", kid}};
  json payload{{"sub", sub}, {"iss", iss}, {"aud", aud}, {"exp", NowUnix() + exp_offset_secs}};
  if (!azp.empty()) payload["azp"] = azp;

  std::string signing_input = EncodeJson(header) + "." + EncodeJson(payload);

  EVP_MD_CTX* mdctx = EVP_MD_CTX_new();
  if (mdctx == nullptr) throw std::runtime_error("EVP_MD_CTX_new failed");
  std::vector<uint8_t> sig;
  if (EVP_DigestSignInit(mdctx, nullptr, EVP_sha256(), nullptr, private_key) != 1) {
    EVP_MD_CTX_free(mdctx);
    throw std::runtime_error("EVP_DigestSignInit failed");
  }
  size_t sig_len = 0;
  if (EVP_DigestSign(mdctx, nullptr, &sig_len,
                      reinterpret_cast<const unsigned char*>(signing_input.data()),
                      signing_input.size()) != 1) {
    EVP_MD_CTX_free(mdctx);
    throw std::runtime_error("EVP_DigestSign(size probe) failed");
  }
  sig.resize(sig_len);
  if (EVP_DigestSign(mdctx, sig.data(), &sig_len,
                      reinterpret_cast<const unsigned char*>(signing_input.data()),
                      signing_input.size()) != 1) {
    EVP_MD_CTX_free(mdctx);
    throw std::runtime_error("EVP_DigestSign failed");
  }
  sig.resize(sig_len);
  EVP_MD_CTX_free(mdctx);

  return signing_input + "." + Base64UrlEncode(sig);
}

}  // namespace backend_cpp::testutil
