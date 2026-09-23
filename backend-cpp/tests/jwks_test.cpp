#include "auth/jwt.hpp"

#include <gtest/gtest.h>
#include <openssl/evp.h>

#include <memory>
#include <nlohmann/json.hpp>

#include "mock_jwks_server.hpp"
#include "test_token_helper.hpp"

// JwksVerifier(RS256)の結合テスト。自プロセス内にモックJWKSサーバーを立て、
// 実際に生成したRSA鍵ペアで署名したJWTを検証する(backend-c/tests/jwks_test.cと同じ設計)
namespace backend_cpp::auth {
namespace {

using json = nlohmann::json;
using backend_cpp::testutil::ExtractRsaPublicComponents;
using backend_cpp::testutil::GenerateRsaKeypair;
using backend_cpp::testutil::MakeRsaToken;
using backend_cpp::testutil::MockJwksServer;

constexpr const char* kIssuer = "http://mock-keycloak.test/realms/training";
constexpr const char* kAudience = "backend";
constexpr unsigned short kPort = 18543;

std::string BuildJwksBody(const std::string& kid, const testutil::RsaPublicComponents& ne) {
  json jwks{{"keys",
             json::array({json{{"kty", "RSA"},
                                {"kid", kid},
                                {"use", "sig"},
                                {"n", ne.n_b64url},
                                {"e", ne.e_b64url}}})}};
  return jwks.dump();
}

struct RsaFixture {
  RsaFixture() {
    key = GenerateRsaKeypair();
    ne = ExtractRsaPublicComponents(key);
  }
  ~RsaFixture() { EVP_PKEY_free(key); }
  EVP_PKEY* key = nullptr;
  testutil::RsaPublicComponents ne;
};

TEST(JwksVerifier, AcceptsValidToken) {
  RsaFixture rsa;
  MockJwksServer server(kPort, BuildJwksBody("kid-1", rsa.ne));

  JwksVerifier verifier(server.Url(), kIssuer, kAudience);
  auto token = MakeRsaToken(rsa.key, "kid-1", kIssuer, kAudience, "99", 3600);
  auto claims = verifier.Verify(token);
  ASSERT_TRUE(claims.has_value());
  EXPECT_EQ(claims->sub, "99");
  EXPECT_EQ(claims->iss, kIssuer);
}

TEST(JwksVerifier, RejectsUnknownKidEvenAfterRefresh) {
  RsaFixture rsa;
  // サーバーが持っているのはkid-1だけ
  MockJwksServer server(kPort, BuildJwksBody("kid-1", rsa.ne));

  JwksVerifier verifier(server.Url(), kIssuer, kAudience);
  // トークンはkid-2を名乗る→ キャッシュに無い→refresh()するが、
  // refresh後もサーバーはkid-1しか返さないため見つからず拒否される
  auto token = MakeRsaToken(rsa.key, "kid-2", kIssuer, kAudience, "99", 3600);
  EXPECT_FALSE(verifier.Verify(token).has_value());
}

TEST(JwksVerifier, RejectsWrongIssuer) {
  RsaFixture rsa;
  MockJwksServer server(kPort, BuildJwksBody("kid-1", rsa.ne));

  JwksVerifier verifier(server.Url(), kIssuer, kAudience);
  auto token = MakeRsaToken(rsa.key, "kid-1", "http://someone-else.test", kAudience, "99", 3600);
  EXPECT_FALSE(verifier.Verify(token).has_value());
}

TEST(JwksVerifier, RejectsTamperedSignature) {
  RsaFixture rsa;
  MockJwksServer server(kPort, BuildJwksBody("kid-1", rsa.ne));

  JwksVerifier verifier(server.Url(), kIssuer, kAudience);
  auto token = MakeRsaToken(rsa.key, "kid-1", kIssuer, kAudience, "99", 3600);
  // 署名部分の先頭の1文字を壊す(末尾側は base64url の余りビットに掛かって
  // デコード結果が変わらないことがあるため、確実に実データへ影響する先頭側を壊す)
  auto last_dot = token.rfind('.');
  ASSERT_NE(last_dot, std::string::npos);
  char& c = token[last_dot + 1];
  c = (c == 'A') ? 'B' : 'A';
  EXPECT_FALSE(verifier.Verify(token).has_value());
}

}  // namespace
}  // namespace backend_cpp::auth
