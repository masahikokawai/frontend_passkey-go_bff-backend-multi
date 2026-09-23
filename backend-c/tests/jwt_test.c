/*
 * JWT/JWKS本実装の単体テスト(DB・Keycloak不要)。backend-rust/src/auth/jwt.rsのテスト
 * モジュール(HmacVerifier + Dispatcherの経路のみを純粋単体テストする方針、JWKS/RS256は
 * jwks_test.cで別途カバーする)と同じ観点で構成している
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "unity.h"

#include "auth/dispatcher.h"
#include "auth/hmac_verifier.h"
#include "auth/jwt.h"
#include "test_token_helper.h"

#define TEST_SECRET "test-secret"
#define TEST_AUD "backend"

void setUp(void) {}
void tearDown(void) {}

static void test_is_local_issuer_matches_hmac_and_rsa_only(void) {
    TEST_ASSERT_TRUE(auth_is_local_issuer(AUTH_LOCAL_HMAC_ISSUER));
    TEST_ASSERT_TRUE(auth_is_local_issuer(AUTH_LOCAL_RSA_ISSUER));
    TEST_ASSERT_FALSE(auth_is_local_issuer("http://localhost:8082/realms/training"));
    TEST_ASSERT_FALSE(auth_is_local_issuer(""));
}

/* backend(Go)のHMACVerifier(hmac_test.go相当)・backend-rustのhmac_verifier_accepts_valid_token
 * と同じ観点: 正しい秘密鍵・iss・audなら検証が通り、subがそのままclaims->subへ伝わる */
static void test_hmac_verifier_accepts_valid_token(void) {
    char *token = make_hmac_token(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD, "42", 3600);
    HmacVerifier *v = hmac_verifier_create(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(0, hmac_verifier_verify(v, token, &claims));
    TEST_ASSERT_NOT_NULL(claims);
    TEST_ASSERT_EQUAL_STRING("42", claims->sub);
    TEST_ASSERT_EQUAL_STRING(AUTH_LOCAL_HMAC_ISSUER, claims->iss);
    claims_destroy(claims);
    hmac_verifier_destroy(v);
    free(token);
}

static void test_hmac_verifier_rejects_wrong_secret(void) {
    char *token = make_hmac_token(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD, "42", 3600);
    HmacVerifier *v = hmac_verifier_create("different-secret", AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(-1, hmac_verifier_verify(v, token, &claims));
    hmac_verifier_destroy(v);
    free(token);
}

static void test_hmac_verifier_rejects_expired_token(void) {
    char *token = make_hmac_token(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD, "42", -3600);
    HmacVerifier *v = hmac_verifier_create(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(-1, hmac_verifier_verify(v, token, &claims));
    hmac_verifier_destroy(v);
    free(token);
}

static void test_hmac_verifier_rejects_wrong_audience(void) {
    char *token = make_hmac_token(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, "someone-else", "42", 3600);
    HmacVerifier *v = hmac_verifier_create(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(-1, hmac_verifier_verify(v, token, &claims));
    hmac_verifier_destroy(v);
    free(token);
}

/* alg混同攻撃対策の回帰テスト: HS256用の鍵/issuerで作ったVerifierに対し、ヘッダのalgだけ
 * "none"に書き換えたトークン(署名部は空)を渡しても拒否されることを確認する
 * (hmac_verifier.cのalgホワイトリストチェック参照) */
static void test_hmac_verifier_rejects_alg_none(void) {
    char *forged = make_hmac_token(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD, "42", 3600);
    /* forgedの先頭セグメント(header_b64)を{"alg":"none","typ":"JWT"}のbase64url表現へ
     * 書き換える(署名部・payload部はそのまま。alg以外は改ざんしなくても、alg自体を
     * 信用してよいならこれだけで検証を素通りできてしまう、というのがこのテストの主題) */
    char none_header_b64[] = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0";
    char *first_dot = strchr(forged, '.');
    size_t remainder_len = strlen(first_dot);
    size_t tampered_len = strlen(none_header_b64) + remainder_len + 1;
    char *tampered = (char *)malloc(tampered_len);
    snprintf(tampered, tampered_len, "%s%s", none_header_b64, first_dot);

    HmacVerifier *v = hmac_verifier_create(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(-1, hmac_verifier_verify(v, tampered, &claims));

    hmac_verifier_destroy(v);
    free(forged);
    free(tampered);
}

/* Dispatcherがissuerで正しいverifierへ振り分けることを確認する
 * (backend(Go)のauthjwt.Dispatcher・backend-rust/backend-cppのDispatcherと同じ2段構造) */
static void test_dispatcher_routes_by_issuer_and_rejects_unknown_issuer(void) {
    Dispatcher *d = dispatcher_create();
    HmacVerifier *v = hmac_verifier_create(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    dispatcher_register(d, AUTH_LOCAL_HMAC_ISSUER, v, hmac_verifier_verify);

    char *good_token = make_hmac_token(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD, "7", 3600);
    char good_header[512];
    snprintf(good_header, sizeof(good_header), "Bearer %s", good_token);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(0, dispatcher_verify(d, good_header, &claims));
    TEST_ASSERT_NOT_NULL(claims);
    TEST_ASSERT_EQUAL_STRING("7", claims->sub);
    claims_destroy(claims);
    free(good_token);

    char *unknown_token = make_hmac_token(TEST_SECRET, "unknown-issuer", TEST_AUD, "7", 3600);
    char unknown_header[512];
    snprintf(unknown_header, sizeof(unknown_header), "Bearer %s", unknown_token);
    Claims *claims2 = NULL;
    TEST_ASSERT_EQUAL_INT(-1, dispatcher_verify(d, unknown_header, &claims2));
    free(unknown_token);

    hmac_verifier_destroy(v);
    dispatcher_destroy(d);
}

static void test_dispatcher_rejects_missing_bearer_prefix(void) {
    Dispatcher *d = dispatcher_create();
    HmacVerifier *v = hmac_verifier_create(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    dispatcher_register(d, AUTH_LOCAL_HMAC_ISSUER, v, hmac_verifier_verify);

    char *token = make_hmac_token(TEST_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD, "7", 3600);
    Claims *claims = NULL;
    /* "Bearer "無しでトークンだけを渡すと拒否される */
    TEST_ASSERT_EQUAL_INT(-1, dispatcher_verify(d, token, &claims));
    /* Authorizationヘッダ自体が無い(NULL)場合も拒否される */
    TEST_ASSERT_EQUAL_INT(-1, dispatcher_verify(d, NULL, &claims));

    free(token);
    hmac_verifier_destroy(v);
    dispatcher_destroy(d);
}

int main(void) {
    UNITY_BEGIN();
    RUN_TEST(test_is_local_issuer_matches_hmac_and_rsa_only);
    RUN_TEST(test_hmac_verifier_accepts_valid_token);
    RUN_TEST(test_hmac_verifier_rejects_wrong_secret);
    RUN_TEST(test_hmac_verifier_rejects_expired_token);
    RUN_TEST(test_hmac_verifier_rejects_wrong_audience);
    RUN_TEST(test_hmac_verifier_rejects_alg_none);
    RUN_TEST(test_dispatcher_routes_by_issuer_and_rejects_unknown_issuer);
    RUN_TEST(test_dispatcher_rejects_missing_bearer_prefix);
    return UNITY_END();
}
