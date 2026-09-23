#pragma once

#include <cstdint>
#include <map>
#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <utility>
#include <vector>

namespace backend_cpp::auth {

inline constexpr const char* kLocalHmacIssuer = "bff-gin-local-hmac";
inline constexpr const char* kLocalRsaIssuer = "bff-gin-local-rsa";

inline bool IsLocalIssuer(const std::string& iss) {
  return iss == kLocalHmacIssuer || iss == kLocalRsaIssuer;
}

struct Claims {
  std::string sub;
  std::string iss;
  std::string azp;
  std::vector<std::string> realm_roles;
};

// backend(Go)のinternal/authjwt、backend-rustのsrc/auth/jwt.rsと同じ設計:
// tokenのiss(署名検証前に覗いた値)で担当Verifierへ振り分ける2段構造
class TokenVerifier {
 public:
  virtual ~TokenVerifier() = default;
  // 検証に成功したらClaimsを返す、失敗したらstd::nullopt
  virtual std::optional<Claims> Verify(const std::string& token) = 0;
};

// ローカルHMAC発行(iss=bff-gin-local-hmac)の検証。HS256、共有シークレット
class HmacVerifier : public TokenVerifier {
 public:
  HmacVerifier(std::string secret, std::string issuer, std::string audience)
      : secret_(std::move(secret)), issuer_(std::move(issuer)), audience_(std::move(audience)) {}
  std::optional<Claims> Verify(const std::string& token) override;

 private:
  std::string secret_;
  std::string issuer_;
  std::string audience_;
};

// Keycloak発行・ローカルRSA発行(iss=bff-gin-local-rsa)共通のJWKSベース検証。RS256
// kid(鍵ID)ごとにpublic keyをキャッシュし、未知のkidが来たときだけJWKSを再取得する
// (backend(Go)のjwks.goと同じ「kid不一致時のみ再取得」戦略)
class JwksVerifier : public TokenVerifier {
 public:
  JwksVerifier(std::string jwks_url, std::string issuer, std::string audience)
      : jwks_url_(std::move(jwks_url)), issuer_(std::move(issuer)), audience_(std::move(audience)) {}
  std::optional<Claims> Verify(const std::string& token) override;

 private:
  bool Refresh();

  std::string jwks_url_;
  std::string issuer_;
  std::string audience_;
  std::mutex mutex_;
  // kid -> (modulus n, exponent e)のbase64urlデコード済みバイト列
  std::map<std::string, std::pair<std::vector<uint8_t>, std::vector<uint8_t>>> keys_;
};

// backend(Go)のDispatcher・backend-rustのDispatcherと同じ:
// 署名検証前にissだけ覗いて、登録済みissuerのVerifierへ振り分ける
class Dispatcher {
 public:
  Dispatcher& Register(std::string issuer, std::shared_ptr<TokenVerifier> verifier);
  // Authorizationヘッダの値("Bearer xxx")を渡す。成功したらuser_id解決に使えるClaimsを返す
  std::optional<Claims> Verify(const std::string& authorization_header);

 private:
  std::map<std::string, std::shared_ptr<TokenVerifier>> verifiers_;
};

}  // namespace backend_cpp::auth
