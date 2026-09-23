#pragma once

#include <openssl/evp.h>

#include <cstdint>
#include <string>
#include <vector>

// テスト専用のJWT組み立てヘルパー。src/auth/base64.hppはデコードのみを提供する
// (本番コードはエンコードを必要としないため)ので、エンコードはテスト側だけに持たせる
namespace backend_cpp::testutil {

std::string Base64UrlEncode(const std::vector<uint8_t>& bytes);

// ローカルHMAC発行(iss=bff-gin-local-hmac)相当の、実際に署名したHS256トークンを組み立てる
// (backend-rust/src/auth/jwt.rsのmake_hmac_token、backend-c/tests/test_token_helper.cの
// 同名関数と同じ役割)。azpは外部公開API(Client Credentials Grant)のテスト向けで、
// 空文字なら発行するトークンにazpクレーム自体を含めない
std::string MakeHmacToken(const std::string& secret, const std::string& iss,
                           const std::string& aud, const std::string& sub,
                           int64_t exp_offset_secs, const std::string& azp = "");

// RSA鍵ペア(2048bit、OpenSSL 3のEVP_RSA_gen)を生成する。呼び出し側がEVP_PKEY_free()すること
EVP_PKEY* GenerateRsaKeypair();

// EVP_PKEYからJWKS(JSON Web Key Set)の1エントリぶんのn/e(base64url、パディング無し)を取り出す
struct RsaPublicComponents {
  std::string n_b64url;
  std::string e_b64url;
};
RsaPublicComponents ExtractRsaPublicComponents(EVP_PKEY* pkey);

// kidを持つRS256トークンを、指定した秘密鍵で実際に署名して組み立てる
std::string MakeRsaToken(EVP_PKEY* private_key, const std::string& kid, const std::string& iss,
                          const std::string& aud, const std::string& sub,
                          int64_t exp_offset_secs, const std::string& azp = "");

}  // namespace backend_cpp::testutil
