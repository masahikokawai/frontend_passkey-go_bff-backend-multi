#include "auth/jwt.hpp"

#include <gtest/gtest.h>

#include <memory>

#include "test_token_helper.hpp"

// backend-rust/src/auth/jwt.rsのmod testsと同じ観点の単体テスト
// (実際に署名したトークンをMakeHmacTokenで組み立て、HmacVerifier/Dispatcherへ通す)
namespace backend_cpp::auth {
namespace {

constexpr const char* kSecret = "test-secret";
constexpr const char* kAudience = "backend";

TEST(IsLocalIssuer, MatchesHmacAndRsaOnly) {
  EXPECT_TRUE(IsLocalIssuer(kLocalHmacIssuer));
  EXPECT_TRUE(IsLocalIssuer(kLocalRsaIssuer));
  EXPECT_FALSE(IsLocalIssuer("http://localhost:8082/realms/training"));
  EXPECT_FALSE(IsLocalIssuer(""));
}

TEST(HmacVerifier, AcceptsValidToken) {
  auto token = testutil::MakeHmacToken(kSecret, kLocalHmacIssuer, kAudience, "42", 3600);
  HmacVerifier verifier(kSecret, kLocalHmacIssuer, kAudience);
  auto claims = verifier.Verify(token);
  ASSERT_TRUE(claims.has_value());
  EXPECT_EQ(claims->sub, "42");
  EXPECT_EQ(claims->iss, kLocalHmacIssuer);
}

TEST(HmacVerifier, RejectsWrongSecret) {
  auto token = testutil::MakeHmacToken(kSecret, kLocalHmacIssuer, kAudience, "42", 3600);
  HmacVerifier verifier("different-secret", kLocalHmacIssuer, kAudience);
  EXPECT_FALSE(verifier.Verify(token).has_value());
}

TEST(HmacVerifier, RejectsExpiredToken) {
  auto token = testutil::MakeHmacToken(kSecret, kLocalHmacIssuer, kAudience, "42", -3600);
  HmacVerifier verifier(kSecret, kLocalHmacIssuer, kAudience);
  EXPECT_FALSE(verifier.Verify(token).has_value());
}

TEST(HmacVerifier, RejectsWrongAudience) {
  auto token = testutil::MakeHmacToken(kSecret, kLocalHmacIssuer, "someone-else", "42", 3600);
  HmacVerifier verifier(kSecret, kLocalHmacIssuer, kAudience);
  EXPECT_FALSE(verifier.Verify(token).has_value());
}

// Dispatcherがissuerで正しいverifierへ振り分けることを確認する
// (backend(Go)のauthjwt.Dispatcher、backend-rust/backend-cのDispatcherと同じ2段構造)
TEST(Dispatcher, RoutesByIssuerAndRejectsUnknownIssuer) {
  Dispatcher dispatcher;
  dispatcher.Register(kLocalHmacIssuer,
                       std::make_shared<HmacVerifier>(kSecret, kLocalHmacIssuer, kAudience));

  auto good_token = testutil::MakeHmacToken(kSecret, kLocalHmacIssuer, kAudience, "7", 3600);
  auto claims = dispatcher.Verify("Bearer " + good_token);
  ASSERT_TRUE(claims.has_value());
  EXPECT_EQ(claims->sub, "7");

  auto unknown_issuer_token = testutil::MakeHmacToken(kSecret, "unknown-issuer", kAudience, "7", 3600);
  EXPECT_FALSE(dispatcher.Verify("Bearer " + unknown_issuer_token).has_value());
}

TEST(Dispatcher, RejectsMissingBearerPrefix) {
  Dispatcher dispatcher;
  dispatcher.Register(kLocalHmacIssuer,
                       std::make_shared<HmacVerifier>(kSecret, kLocalHmacIssuer, kAudience));
  auto token = testutil::MakeHmacToken(kSecret, kLocalHmacIssuer, kAudience, "7", 3600);
  EXPECT_FALSE(dispatcher.Verify(token).has_value());  // "Bearer "無し
}

}  // namespace
}  // namespace backend_cpp::auth
