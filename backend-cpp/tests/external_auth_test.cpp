#include "external/external_handler.hpp"

#include <gtest/gtest.h>
#include <openssl/evp.h>

#include <memory>
#include <nlohmann/json.hpp>

#include "mock_jwks_server.hpp"
#include "test_token_helper.hpp"

// 外部公開API(CONTRACT.mdセクション11)のRequireExternalClientAuthを検証する単体テスト。
// DB・実HTTPサーバーは不要(モックJWKSサーバーのみ、jwks_test.cppと同じ設計)。
// backend-javaのExternalAuthTestと同じ観点(7ケース)
namespace backend_cpp::external {
namespace {

using json = nlohmann::json;
using backend_cpp::testutil::ExtractRsaPublicComponents;
using backend_cpp::testutil::GenerateRsaKeypair;
using backend_cpp::testutil::MakeHmacToken;
using backend_cpp::testutil::MakeRsaToken;
using backend_cpp::testutil::MockJwksServer;

constexpr const char* kExternalClientId = "external-api-client";
constexpr const char* kKeycloakIssuer = "http://mock-keycloak.test/realms/training";
constexpr const char* kAudience = "backend";
constexpr const char* kHmacSecret = "test-secret-at-least-32-bytes-long!!";
constexpr unsigned short kJwksPort = 18545;

struct RsaFixture {
  RsaFixture() {
    key = GenerateRsaKeypair();
    ne = ExtractRsaPublicComponents(key);
  }
  ~RsaFixture() { EVP_PKEY_free(key); }
  EVP_PKEY* key = nullptr;
  testutil::RsaPublicComponents ne;
};

std::string BuildJwksBody(const std::string& kid, const testutil::RsaPublicComponents& ne) {
  json jwks{{"keys",
             json::array({json{{"kty", "RSA"},
                                {"kid", kid},
                                {"use", "sig"},
                                {"n", ne.n_b64url},
                                {"e", ne.e_b64url}}})}};
  return jwks.dump();
}

TEST(RequireExternalClientAuth, AcceptsKeycloakTokenWithMatchingAzp) {
  RsaFixture rsa;
  MockJwksServer server(kJwksPort, BuildJwksBody("kid-1", rsa.ne));
  auth::Dispatcher dispatcher;
  dispatcher.Register(kKeycloakIssuer,
                       std::make_shared<auth::JwksVerifier>(server.Url(), kKeycloakIssuer, kAudience));

  auto token = MakeRsaToken(rsa.key, "kid-1", kKeycloakIssuer, kAudience, "some-keycloak-sub", 3600,
                             kExternalClientId);
  auto claims = RequireExternalClientAuth(dispatcher, "Bearer " + token, kExternalClientId);
  ASSERT_TRUE(claims.has_value());
  EXPECT_EQ(claims->sub, "some-keycloak-sub");
}

TEST(RequireExternalClientAuth, RejectsMissingAuthorizationHeader) {
  auth::Dispatcher dispatcher;
  auto claims = RequireExternalClientAuth(dispatcher, std::nullopt, kExternalClientId);
  EXPECT_FALSE(claims.has_value());
}

TEST(RequireExternalClientAuth, RejectsWrongAzp) {
  RsaFixture rsa;
  MockJwksServer server(kJwksPort, BuildJwksBody("kid-1", rsa.ne));
  auth::Dispatcher dispatcher;
  dispatcher.Register(kKeycloakIssuer,
                       std::make_shared<auth::JwksVerifier>(server.Url(), kKeycloakIssuer, kAudience));

  auto token =
      MakeRsaToken(rsa.key, "kid-1", kKeycloakIssuer, kAudience, "sub", 3600, "some-other-client");
  auto claims = RequireExternalClientAuth(dispatcher, "Bearer " + token, kExternalClientId);
  EXPECT_FALSE(claims.has_value());
}

TEST(RequireExternalClientAuth, RejectsMissingAzp) {
  RsaFixture rsa;
  MockJwksServer server(kJwksPort, BuildJwksBody("kid-1", rsa.ne));
  auth::Dispatcher dispatcher;
  dispatcher.Register(kKeycloakIssuer,
                       std::make_shared<auth::JwksVerifier>(server.Url(), kKeycloakIssuer, kAudience));

  // azp無し(既定の空文字のまま)のトークン
  auto token = MakeRsaToken(rsa.key, "kid-1", kKeycloakIssuer, kAudience, "sub", 3600);
  auto claims = RequireExternalClientAuth(dispatcher, "Bearer " + token, kExternalClientId);
  EXPECT_FALSE(claims.has_value());
}

// 最も見落としやすいケース: ローカルHMAC発行のトークンは、署名自体は正しく検証できても
// (azpが偶然一致していても)、外部公開APIでは拒否されなければならない
// (Client Credentials Grant、つまりKeycloak発行のみを受け付ける設計)
TEST(RequireExternalClientAuth, RejectsLocalHmacIssuerEvenWithCorrectAzp) {
  auth::Dispatcher dispatcher;
  dispatcher.Register(auth::kLocalHmacIssuer,
                       std::make_shared<auth::HmacVerifier>(kHmacSecret, auth::kLocalHmacIssuer,
                                                              kAudience));

  auto token = MakeHmacToken(kHmacSecret, auth::kLocalHmacIssuer, kAudience, "1", 3600,
                              kExternalClientId);
  auto claims = RequireExternalClientAuth(dispatcher, "Bearer " + token, kExternalClientId);
  EXPECT_FALSE(claims.has_value());
}

TEST(RequireExternalClientAuth, RejectsLocalRsaIssuerEvenWithCorrectAzp) {
  RsaFixture rsa;
  MockJwksServer server(kJwksPort, BuildJwksBody("kid-1", rsa.ne));
  auth::Dispatcher dispatcher;
  dispatcher.Register(auth::kLocalRsaIssuer,
                       std::make_shared<auth::JwksVerifier>(server.Url(), auth::kLocalRsaIssuer,
                                                              kAudience));

  auto token = MakeRsaToken(rsa.key, "kid-1", auth::kLocalRsaIssuer, kAudience, "1", 3600,
                             kExternalClientId);
  auto claims = RequireExternalClientAuth(dispatcher, "Bearer " + token, kExternalClientId);
  EXPECT_FALSE(claims.has_value());
}

TEST(RequireExternalClientAuth, RejectsUnknownIssuer) {
  auth::Dispatcher dispatcher;  // どのissuerも登録しない
  auto token = MakeHmacToken("test-secret", "https://unknown-issuer.example", kAudience, "1", 3600,
                              kExternalClientId);
  auto claims = RequireExternalClientAuth(dispatcher, "Bearer " + token, kExternalClientId);
  EXPECT_FALSE(claims.has_value());
}

}  // namespace
}  // namespace backend_cpp::external
