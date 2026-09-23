/*
 * JWKS(RS256)経路の単体テスト。backend-rust/backend-cppにはこの経路の自動テストが無い
 * (Keycloak起動が必要なためe2eでのみカバーしている)が、backend-cはCivetWebが既に
 * リンク済みの依存であることを活かし、このテストプロセス自身の中に「本物のJWKSレスポンスを
 * 返すモックHTTPサーバー」を1つ立てて、JwksVerifierが実際にHTTP GET+RSA検証まで行う経路を
 * 自動テストできるようにしている(DB・実Keycloak不要)
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <civetweb.h>
#include <cjson/cJSON.h>
#include <openssl/bn.h>
#include <openssl/evp.h>
#include <openssl/rsa.h>

#include "unity.h"

#include "auth/base64url.h"
#include "auth/dispatcher.h"
#include "auth/external_auth.h"
#include "auth/hmac_verifier.h"
#include "auth/jwks_verifier.h"
#include "auth/jwt.h"
#include "test_token_helper.h"

#define TEST_KID "test-kid-1"
#define TEST_ISS "https://mock-issuer.example.test"
#define TEST_AUD "backend"
#define TEST_EXTERNAL_CLIENT_ID "external-api-client"
#define TEST_LOCAL_HMAC_SECRET "external-auth-test-hmac-secret"

static EVP_PKEY *g_keypair = NULL;
static char *g_jwks_body = NULL;
static struct mg_context *g_mock_ctx = NULL;
static int g_mock_port = 0;

static char *bignum_to_base64url(const BIGNUM *bn) {
    int len = BN_num_bytes(bn);
    uint8_t *buf = (uint8_t *)malloc((size_t)len > 0 ? (size_t)len : 1);
    BN_bn2bin(bn, buf);
    char *encoded = base64url_encode(buf, (size_t)len);
    free(buf);
    return encoded;
}

/* 2048bit RSA鍵を1組生成し、そのn/eからJWKSレスポンス本体(JSON文字列)を組み立てる */
static void build_mock_jwks(void) {
    EVP_PKEY_CTX *ctx = EVP_PKEY_CTX_new_id(EVP_PKEY_RSA, NULL);
    EVP_PKEY_keygen_init(ctx);
    EVP_PKEY_CTX_set_rsa_keygen_bits(ctx, 2048);
    EVP_PKEY_keygen(ctx, &g_keypair);
    EVP_PKEY_CTX_free(ctx);

    BIGNUM *n = NULL;
    BIGNUM *e = NULL;
    EVP_PKEY_get_bn_param(g_keypair, "n", &n);
    EVP_PKEY_get_bn_param(g_keypair, "e", &e);
    char *n_b64 = bignum_to_base64url(n);
    char *e_b64 = bignum_to_base64url(e);
    BN_free(n);
    BN_free(e);

    cJSON *root = cJSON_CreateObject();
    cJSON *keys = cJSON_AddArrayToObject(root, "keys");
    cJSON *key = cJSON_CreateObject();
    cJSON_AddStringToObject(key, "kty", "RSA");
    cJSON_AddStringToObject(key, "use", "sig");
    cJSON_AddStringToObject(key, "kid", TEST_KID);
    cJSON_AddStringToObject(key, "n", n_b64);
    cJSON_AddStringToObject(key, "e", e_b64);
    cJSON_AddItemToArray(keys, key);

    g_jwks_body = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    free(n_b64);
    free(e_b64);
}

static int jwks_handler(struct mg_connection *conn, void *cbdata) {
    (void)cbdata;
    mg_printf(conn,
              "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %zu\r\n\r\n",
              strlen(g_jwks_body));
    mg_write(conn, g_jwks_body, strlen(g_jwks_body));
    return 1;
}

void setUp(void) {}
void tearDown(void) {}

/* kidが一致する正しい署名のトークンを検証できることの確認(初回アクセスでJWKSを取得し、
 * n/eからEVP_PKEYを組み立ててRS256署名検証を行う経路全体を通す) */
static void test_jwks_verifier_accepts_valid_rs256_token(void) {
    char url[128];
    snprintf(url, sizeof(url), "http://127.0.0.1:%d/jwks", g_mock_port);
    JwksVerifier *v = jwks_verifier_create(url, TEST_ISS, TEST_AUD);

    char *token = make_rsa_token(g_keypair, TEST_KID, TEST_ISS, TEST_AUD, "99", 3600);
    TEST_ASSERT_NOT_NULL(token);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(0, jwks_verifier_verify(v, token, &claims));
    TEST_ASSERT_NOT_NULL(claims);
    TEST_ASSERT_EQUAL_STRING("99", claims->sub);
    claims_destroy(claims);

    free(token);
    jwks_verifier_destroy(v);
}

/* 存在しないkidを指すトークンは、1度JWKSを再取得してもなお見つからないため拒否される
 * (jwks_verifier.cのfind_key_copy→refresh→再find_key_copyという「kid不一致時のみ
 * 再取得」戦略の確認) */
static void test_jwks_verifier_rejects_unknown_kid_after_refresh(void) {
    char url[128];
    snprintf(url, sizeof(url), "http://127.0.0.1:%d/jwks", g_mock_port);
    JwksVerifier *v = jwks_verifier_create(url, TEST_ISS, TEST_AUD);

    char *token = make_rsa_token(g_keypair, "nonexistent-kid", TEST_ISS, TEST_AUD, "99", 3600);
    TEST_ASSERT_NOT_NULL(token);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(-1, jwks_verifier_verify(v, token, &claims));

    free(token);
    jwks_verifier_destroy(v);
}

/* 署名が正しくてもiss/audが期待値と異なれば拒否されることの確認 */
static void test_jwks_verifier_rejects_wrong_issuer(void) {
    char url[128];
    snprintf(url, sizeof(url), "http://127.0.0.1:%d/jwks", g_mock_port);
    JwksVerifier *v = jwks_verifier_create(url, TEST_ISS, TEST_AUD);

    char *token = make_rsa_token(g_keypair, TEST_KID, "https://someone-else.example.test", TEST_AUD,
                                  "99", 3600);
    TEST_ASSERT_NOT_NULL(token);
    Claims *claims = NULL;
    TEST_ASSERT_EQUAL_INT(-1, jwks_verifier_verify(v, token, &claims));

    free(token);
    jwks_verifier_destroy(v);
}

/* CONTRACT.mdセクション11「RequireExternalClientAuth」相当のテスト。
 * auth_module_init()一式を起動せず、このテストだけで完結する最小限のDispatcher
 * (Keycloak相当のJwksVerifier1つ+ローカルHMACVerifier1つ)を組み立てて検証する */
static Dispatcher *build_test_dispatcher(void) {
    char url[128];
    snprintf(url, sizeof(url), "http://127.0.0.1:%d/jwks", g_mock_port);

    Dispatcher *d = dispatcher_create();
    JwksVerifier *keycloak_like = jwks_verifier_create(url, TEST_ISS, TEST_AUD);
    HmacVerifier *local_hmac =
        hmac_verifier_create(TEST_LOCAL_HMAC_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    dispatcher_register(d, TEST_ISS, keycloak_like, jwks_verifier_verify);
    dispatcher_register(d, AUTH_LOCAL_HMAC_ISSUER, local_hmac, hmac_verifier_verify);
    return d;
    /* keycloak_like/local_hmacは意図的にリークする(テストプロセス終了まで生存すればよい、
     * dispatcher_destroyはverifier自体を解放しないため呼び出し側が別途管理する設計。
     * 他のjwks_test.cのテストと同じくプロセス終了時にOSへ回収される単発テストのため
     * 明示的なdestroyは省略している) */
}

/* azpがexternal_api_client_idと一致する正しいKeycloak発行トークンは受理される */
static void test_require_external_client_accepts_matching_azp(void) {
    Dispatcher *d = build_test_dispatcher();
    char *token = make_rsa_token_with_azp(g_keypair, TEST_KID, TEST_ISS, TEST_AUD, "99",
                                           TEST_EXTERNAL_CLIENT_ID, 3600);
    TEST_ASSERT_NOT_NULL(token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", token);
    TEST_ASSERT_EQUAL_INT(
        TASK_OK, auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(token);
    dispatcher_destroy(d);
}

/* azpが一致しない(別クライアント/未設定)場合は署名が正しくても拒否される */
static void test_require_external_client_rejects_wrong_azp(void) {
    Dispatcher *d = build_test_dispatcher();
    char *wrong_azp_token = make_rsa_token_with_azp(g_keypair, TEST_KID, TEST_ISS, TEST_AUD, "99",
                                                     "someone-else-client", 3600);
    char *no_azp_token =
        make_rsa_token(g_keypair, TEST_KID, TEST_ISS, TEST_AUD, "99", 3600); /* azpクレーム無し */
    TEST_ASSERT_NOT_NULL(wrong_azp_token);
    TEST_ASSERT_NOT_NULL(no_azp_token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", wrong_azp_token);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    snprintf(auth_header, sizeof(auth_header), "Bearer %s", no_azp_token);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(wrong_azp_token);
    free(no_azp_token);
    dispatcher_destroy(d);
}

/* ローカルHMAC発行のJWTは、署名・iss・aud・exp全てが正しくても外部公開APIでは
 * 拒否される(内部REST/gRPC専用、CONTRACT.mdセクション11「Keycloak発行のみ許可」) */
static void test_require_external_client_rejects_local_hmac_issuer(void) {
    Dispatcher *d = build_test_dispatcher();
    char *token = make_hmac_token(TEST_LOCAL_HMAC_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD, "7",
                                   3600);
    TEST_ASSERT_NOT_NULL(token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", token);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(token);
    dispatcher_destroy(d);
}

int main(void) {
    build_mock_jwks();

    mg_init_library(0);
    /* "0"を指定するとOSが空きポートを1つ割り当てる(このプロセス内だけで完結するテストの
     * ため、固定ポートにして他のテスト実行と衝突するのを避ける) */
    const char *options[] = {"listening_ports", "0", "num_threads", "4", NULL};
    g_mock_ctx = mg_start(NULL, NULL, options);
    if (g_mock_ctx == NULL) {
        fprintf(stderr, "mock jwks server mg_start failed\n");
        return 1;
    }
    mg_set_request_handler(g_mock_ctx, "/jwks", jwks_handler, NULL);

    struct mg_server_ports port_list[8];
    int n = mg_get_server_ports(g_mock_ctx, 8, port_list);
    g_mock_port = (n > 0) ? port_list[0].port : 0;
    if (g_mock_port == 0) {
        fprintf(stderr, "failed to determine mock jwks server port\n");
        return 1;
    }

    UNITY_BEGIN();
    RUN_TEST(test_jwks_verifier_accepts_valid_rs256_token);
    RUN_TEST(test_jwks_verifier_rejects_unknown_kid_after_refresh);
    RUN_TEST(test_jwks_verifier_rejects_wrong_issuer);
    RUN_TEST(test_require_external_client_accepts_matching_azp);
    RUN_TEST(test_require_external_client_rejects_wrong_azp);
    RUN_TEST(test_require_external_client_rejects_local_hmac_issuer);
    int result = UNITY_END();

    mg_stop(g_mock_ctx);
    mg_exit_library();
    EVP_PKEY_free(g_keypair);
    free(g_jwks_body);
    return result;
}
