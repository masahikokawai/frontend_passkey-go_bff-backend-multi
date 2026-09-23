#include "auth/jwt.hpp"

#include <openssl/bn.h>
#include <openssl/evp.h>
#include <openssl/hmac.h>
#include <openssl/param_build.h>

#include <boost/asio/connect.hpp>
#include <boost/asio/ip/tcp.hpp>
#include <boost/beast/core.hpp>
#include <boost/beast/http.hpp>
#include <chrono>
#include <cstring>
#include <iostream>
#include <nlohmann/json.hpp>
#include <sstream>

#include "auth/base64.hpp"
#include "common/logging.hpp"

namespace backend_cpp::auth {

using json = nlohmann::json;

namespace {

// "header.payload.signature" を3分割する。区切りが無ければstd::nullopt
struct RawParts {
  std::string header_b64, payload_b64, signature_b64;
  std::string signing_input;  // header_b64 + "." + payload_b64(署名対象)
};

std::optional<RawParts> SplitJwt(const std::string& token) {
  auto p1 = token.find('.');
  if (p1 == std::string::npos) return std::nullopt;
  auto p2 = token.find('.', p1 + 1);
  if (p2 == std::string::npos) return std::nullopt;
  RawParts parts;
  parts.header_b64 = token.substr(0, p1);
  parts.payload_b64 = token.substr(p1 + 1, p2 - p1 - 1);
  parts.signature_b64 = token.substr(p2 + 1);
  parts.signing_input = token.substr(0, p2);
  return parts;
}

std::string BytesToString(const std::vector<uint8_t>& v) {
  return std::string(reinterpret_cast<const char*>(v.data()), v.size());
}

// 署名検証前にexp/nbfを見る前段として、まずclaimsをパースするだけの共通処理
std::optional<json> DecodePayloadJson(const std::string& payload_b64) {
  auto raw = Base64UrlDecode(payload_b64);
  try {
    return json::parse(BytesToString(raw));
  } catch (...) {
    return std::nullopt;
  }
}

Claims ClaimsFromJson(const json& j) {
  Claims c;
  c.sub = j.value("sub", "");
  c.iss = j.value("iss", "");
  c.azp = j.value("azp", "");
  if (j.contains("realm_access") && j["realm_access"].contains("roles")) {
    for (const auto& r : j["realm_access"]["roles"]) {
      if (r.is_string()) c.realm_roles.push_back(r.get<std::string>());
    }
  }
  return c;
}

// audは文字列 or 文字列配列のことがある(RFC 7519)。いずれかがexpectedと一致すればよい
bool AudienceMatches(const json& j, const std::string& expected) {
  if (!j.contains("aud")) return false;
  const auto& aud = j["aud"];
  if (aud.is_string()) return aud.get<std::string>() == expected;
  if (aud.is_array()) {
    for (const auto& v : aud) {
      if (v.is_string() && v.get<std::string>() == expected) return true;
    }
  }
  return false;
}

bool NotExpired(const json& j) {
  if (!j.contains("exp")) return false;
  int64_t exp = j["exp"].get<int64_t>();
  auto now = std::chrono::duration_cast<std::chrono::seconds>(
                 std::chrono::system_clock::now().time_since_epoch())
                 .count();
  return now < exp;
}

}  // namespace

// ---- HmacVerifier(HS256) ----

std::optional<Claims> HmacVerifier::Verify(const std::string& token) {
  auto parts = SplitJwt(token);
  if (!parts) return std::nullopt;

  auto payload_json = DecodePayloadJson(parts->payload_b64);
  if (!payload_json) return std::nullopt;
  if (payload_json->value("iss", "") != issuer_) return std::nullopt;
  if (!AudienceMatches(*payload_json, audience_)) return std::nullopt;
  if (!NotExpired(*payload_json)) return std::nullopt;

  unsigned char mac[EVP_MAX_MD_SIZE];
  unsigned int mac_len = 0;
  HMAC(EVP_sha256(), secret_.data(), static_cast<int>(secret_.size()),
       reinterpret_cast<const unsigned char*>(parts->signing_input.data()),
       parts->signing_input.size(), mac, &mac_len);

  auto expected_sig = Base64UrlDecode(parts->signature_b64);
  if (expected_sig.size() != mac_len) return std::nullopt;
  if (CRYPTO_memcmp(mac, expected_sig.data(), mac_len) != 0) return std::nullopt;

  return ClaimsFromJson(*payload_json);
}

// ---- JwksVerifier(RS256) ----

namespace {

// bffやKeycloakのJWKSエンドポイントへの単純な同期HTTP GET
// (呼び出しはFeature Flagポーラーと同じ背景スレッドから行う想定、io_contextはブロックしない)
std::optional<std::string> HttpGet(const std::string& url) {
  namespace beast = boost::beast;
  namespace http = beast::http;
  namespace asio = boost::asio;
  using asio::ip::tcp;

  try {
    // 超簡易URLパース: http://host:port/path のみ対応(このプロジェクト内で十分)
    std::string rest = url;
    const std::string scheme = "http://";
    if (rest.rfind(scheme, 0) == 0) rest = rest.substr(scheme.size());
    auto slash = rest.find('/');
    std::string host_port = slash == std::string::npos ? rest : rest.substr(0, slash);
    std::string target = slash == std::string::npos ? "/" : rest.substr(slash);
    std::string host = host_port;
    std::string port = "80";
    auto colon = host_port.find(':');
    if (colon != std::string::npos) {
      host = host_port.substr(0, colon);
      port = host_port.substr(colon + 1);
    }

    asio::io_context ioc;
    tcp::resolver resolver(ioc);
    beast::tcp_stream stream(ioc);
    auto const results = resolver.resolve(host, port);
    stream.connect(results);

    http::request<http::string_body> req{http::verb::get, target, 11};
    req.set(http::field::host, host);
    req.set(http::field::user_agent, "backend-cpp");
    http::write(stream, req);

    beast::flat_buffer buffer;
    http::response<http::string_body> res;
    http::read(stream, buffer, res);

    beast::error_code ec;
    stream.socket().shutdown(tcp::socket::shutdown_both, ec);
    if (res.result_int() != 200) return std::nullopt;
    return res.body();
  } catch (const std::exception& e) {
    std::cerr << "level=ERROR msg=\"JWKS取得失敗\" url=\"" << url << "\" error=\"" << e.what()
              << "\"" << std::endl;
    return std::nullopt;
  }
}

}  // namespace

bool JwksVerifier::Refresh() {
  auto body = HttpGet(jwks_url_);
  if (!body) return false;
  try {
    auto j = json::parse(*body);
    std::map<std::string, std::pair<std::vector<uint8_t>, std::vector<uint8_t>>> new_keys;
    for (const auto& k : j["keys"]) {
      if (k.value("kty", "") != "RSA") continue;
      std::string use = k.value("use", "");
      if (!use.empty() && use != "sig") continue;
      std::string kid = k.value("kid", "");
      std::string n = k.value("n", "");
      std::string e = k.value("e", "");
      if (kid.empty() || n.empty() || e.empty()) continue;
      new_keys[kid] = {Base64UrlDecode(n), Base64UrlDecode(e)};
    }
    std::lock_guard<std::mutex> lock(mutex_);
    keys_ = std::move(new_keys);
    return true;
  } catch (...) {
    return false;
  }
}

namespace {

// n(modulus)・e(exponent)のバイト列からRSA公開鍵のEVP_PKEYを組み立てる(OpenSSL 3 API)
EVP_PKEY* BuildRsaPublicKey(const std::vector<uint8_t>& n, const std::vector<uint8_t>& e) {
  BIGNUM* bn_n = BN_bin2bn(n.data(), static_cast<int>(n.size()), nullptr);
  BIGNUM* bn_e = BN_bin2bn(e.data(), static_cast<int>(e.size()), nullptr);
  if (!bn_n || !bn_e) return nullptr;

  OSSL_PARAM_BLD* bld = OSSL_PARAM_BLD_new();
  OSSL_PARAM_BLD_push_BN(bld, "n", bn_n);
  OSSL_PARAM_BLD_push_BN(bld, "e", bn_e);
  OSSL_PARAM* params = OSSL_PARAM_BLD_to_param(bld);

  EVP_PKEY_CTX* ctx = EVP_PKEY_CTX_new_from_name(nullptr, "RSA", nullptr);
  EVP_PKEY* pkey = nullptr;
  if (ctx && EVP_PKEY_fromdata_init(ctx) > 0) {
    EVP_PKEY_fromdata(ctx, &pkey, EVP_PKEY_PUBLIC_KEY, params);
  }
  if (ctx) EVP_PKEY_CTX_free(ctx);
  OSSL_PARAM_free(params);
  OSSL_PARAM_BLD_free(bld);
  BN_free(bn_n);
  BN_free(bn_e);
  return pkey;
}

bool VerifyRs256(EVP_PKEY* pkey, const std::string& signing_input,
                  const std::vector<uint8_t>& signature) {
  EVP_MD_CTX* mdctx = EVP_MD_CTX_new();
  bool ok = false;
  if (EVP_DigestVerifyInit(mdctx, nullptr, EVP_sha256(), nullptr, pkey) == 1) {
    ok = EVP_DigestVerify(mdctx, signature.data(), signature.size(),
                           reinterpret_cast<const unsigned char*>(signing_input.data()),
                           signing_input.size()) == 1;
  }
  EVP_MD_CTX_free(mdctx);
  return ok;
}

}  // namespace

std::optional<Claims> JwksVerifier::Verify(const std::string& token) {
  auto parts = SplitJwt(token);
  if (!parts) return std::nullopt;

  auto header_raw = Base64UrlDecode(parts->header_b64);
  json header_json;
  try {
    header_json = json::parse(BytesToString(header_raw));
  } catch (...) {
    return std::nullopt;
  }
  std::string kid = header_json.value("kid", "");
  if (kid.empty()) return std::nullopt;

  auto payload_json = DecodePayloadJson(parts->payload_b64);
  if (!payload_json) return std::nullopt;
  if (payload_json->value("iss", "") != issuer_) return std::nullopt;
  if (!AudienceMatches(*payload_json, audience_)) return std::nullopt;
  if (!NotExpired(*payload_json)) return std::nullopt;

  std::pair<std::vector<uint8_t>, std::vector<uint8_t>> ne;
  {
    std::lock_guard<std::mutex> lock(mutex_);
    auto it = keys_.find(kid);
    if (it != keys_.end()) ne = it->second;
  }
  if (ne.first.empty()) {
    // kid不一致 → 一度だけ再取得(backend(Go)のjwks.goと同じ戦略)
    common::LogDebug("auth debug: jwks refresh triggered url=" + jwks_url_ + " kid=" + kid);
    if (!Refresh()) return std::nullopt;
    std::lock_guard<std::mutex> lock(mutex_);
    auto it = keys_.find(kid);
    if (it == keys_.end()) return std::nullopt;
    ne = it->second;
  }

  EVP_PKEY* pkey = BuildRsaPublicKey(ne.first, ne.second);
  if (!pkey) return std::nullopt;
  auto signature = Base64UrlDecode(parts->signature_b64);
  bool ok = VerifyRs256(pkey, parts->signing_input, signature);
  EVP_PKEY_free(pkey);
  if (!ok) return std::nullopt;

  return ClaimsFromJson(*payload_json);
}

// ---- Dispatcher ----

Dispatcher& Dispatcher::Register(std::string issuer, std::shared_ptr<TokenVerifier> verifier) {
  verifiers_[std::move(issuer)] = std::move(verifier);
  return *this;
}

std::optional<Claims> Dispatcher::Verify(const std::string& authorization_header) {
  const std::string prefix = "Bearer ";
  if (authorization_header.rfind(prefix, 0) != 0) return std::nullopt;
  std::string token = authorization_header.substr(prefix.size());

  auto parts = SplitJwt(token);
  if (!parts) return std::nullopt;
  // 署名検証前に、まずissだけを覗いて担当Verifierを選ぶ(2段構造)
  auto payload_json = DecodePayloadJson(parts->payload_b64);
  if (!payload_json) return std::nullopt;
  std::string iss = payload_json->value("iss", "");
  auto it = verifiers_.find(iss);
  if (it == verifiers_.end()) return std::nullopt;
  return it->second->Verify(token);
}

}  // namespace backend_cpp::auth
