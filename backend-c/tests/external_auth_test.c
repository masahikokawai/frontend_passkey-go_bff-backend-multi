/*
 * 外部公開API(CONTRACT.mdセクション11「認証: Client Credentials Grant」)の
 * auth_require_external_client(src/auth/external_auth.c)単体テスト。
 * jwks_test.cはJWKS経路そのものの検証(RS256署名・kidキャッシュ)が主眼で、
 * RequireExternalClientAuth自体は代表的な3ケースのみを併せて確認しているに留まる。
 * このファイルはbackend-java/src/test/java/com/bffgin/backend/auth/ExternalAuthTest.java
 * (gold standard、CONTRACT.mdセクション20.5のワイヤー契約パリティ)と1対1で対応する
 * 7ケースを揃え、外部公開API認証だけに閉じた単体テストとして完結させる。
 * DB・実Keycloak・実サーバーは不要(jwks_test.cと同じくこのプロセス内にモックJWKS
 * サーバーを1つ立てるのみ)
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
#define TEST_ISS "https://mock-issuer.example.test" /* Keycloak相当 */
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

/* jwks_test.cのbuild_mock_jwksと同じ役割(2048bit RSA鍵1組からJWKSレスポンスを組み立てる)。
 * このプロセス専用のモックサーバーであり実Keycloakには一切依存しない */
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

/* Keycloak相当(TEST_ISS)・ローカルHMAC・ローカルRSAの3issuerを登録したDispatcherを
 * 組み立てる(auth_module_init()一式は起動せず、このテストだけで完結させる)。
 * ローカルRSA発行もauth_module.c同様RS256/JWKS経路のため、同じモックJWKSサーバーを
 * 使い回す(issuer文字列が異なるだけで鍵は共通) */
static Dispatcher *build_test_dispatcher(void) {
    char url[128];
    snprintf(url, sizeof(url), "http://127.0.0.1:%d/jwks", g_mock_port);

    Dispatcher *d = dispatcher_create();
    JwksVerifier *keycloak_like = jwks_verifier_create(url, TEST_ISS, TEST_AUD);
    JwksVerifier *local_rsa = jwks_verifier_create(url, AUTH_LOCAL_RSA_ISSUER, TEST_AUD);
    HmacVerifier *local_hmac =
        hmac_verifier_create(TEST_LOCAL_HMAC_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD);
    dispatcher_register(d, TEST_ISS, keycloak_like, jwks_verifier_verify);
    dispatcher_register(d, AUTH_LOCAL_RSA_ISSUER, local_rsa, jwks_verifier_verify);
    dispatcher_register(d, AUTH_LOCAL_HMAC_ISSUER, local_hmac, hmac_verifier_verify);
    return d;
    /* keycloak_like/local_rsa/local_hmacは意図的にリークする(jwks_test.cの
     * build_test_dispatcherと同じ理由: dispatcher_destroyはverifier自体を解放しない設計、
     * 単発テストプロセス終了時にOSへ回収される) */
}

/* Authorizationヘッダ自体が無ければ、Dispatcherへ問い合わせるまでもなく即座に拒否される */
static void test_rejects_missing_authorization_header(void) {
    Dispatcher *d = dispatcher_create();
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, NULL, TEST_EXTERNAL_CLIENT_ID));
    dispatcher_destroy(d);
}

/* Dispatcherに登録の無いissuerのトークンは、azpを見るまでもなく拒否される
 * (dispatcher_verify自体が失敗するため) */
static void test_rejects_unknown_issuer(void) {
    Dispatcher *d = dispatcher_create();
    char *token = make_hmac_token_with_azp("whatever-secret", "https://unknown-issuer.example.test",
                                            TEST_AUD, "1", TEST_EXTERNAL_CLIENT_ID, 3600);
    TEST_ASSERT_NOT_NULL(token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", token);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(token);
    dispatcher_destroy(d);
}

/* azpがexternal_api_client_idと一致する正しいKeycloak発行(Client Credentials Grant)の
 * トークンは受理される(唯一のTASK_OKケース) */
static void test_accepts_valid_keycloak_client_credentials_token(void) {
    Dispatcher *d = build_test_dispatcher();
    char *token = make_rsa_token_with_azp(g_keypair, TEST_KID, TEST_ISS, TEST_AUD, "99",
                                           TEST_EXTERNAL_CLIENT_ID, 3600);
    TEST_ASSERT_NOT_NULL(token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", token);
    TEST_ASSERT_EQUAL_INT(TASK_OK,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(token);
    dispatcher_destroy(d);
}

/* azpが別クライアントを指す場合は、署名・iss・audが全て正しくても拒否される */
static void test_rejects_wrong_azp(void) {
    Dispatcher *d = build_test_dispatcher();
    char *token = make_rsa_token_with_azp(g_keypair, TEST_KID, TEST_ISS, TEST_AUD, "99",
                                           "some-other-client", 3600);
    TEST_ASSERT_NOT_NULL(token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", token);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(token);
    dispatcher_destroy(d);
}

/* azpクレーム自体が無いトークンも同様に拒否される(wrong_azpとは別の分岐: strcmpの
 * 引数がNULLになり得るケースを別テストとして明示しておく) */
static void test_rejects_missing_azp(void) {
    Dispatcher *d = build_test_dispatcher();
    char *token = make_rsa_token(g_keypair, TEST_KID, TEST_ISS, TEST_AUD, "99", 3600);
    TEST_ASSERT_NOT_NULL(token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", token);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(token);
    dispatcher_destroy(d);
}

/* 最も見落としやすいケース: ローカルHMAC発行のトークンは、署名自体は正しく検証でき
 * azpも一致していても、外部公開APIでは拒否されなければならない
 * (Client Credentials Grant、つまりKeycloak発行のみを受け付ける設計、
 * external_auth.hのauth_is_local_issuerチェック参照) */
static void test_rejects_local_hmac_issuer_even_with_correct_azp(void) {
    Dispatcher *d = build_test_dispatcher();
    char *token = make_hmac_token_with_azp(TEST_LOCAL_HMAC_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUD,
                                            "7", TEST_EXTERNAL_CLIENT_ID, 3600);
    TEST_ASSERT_NOT_NULL(token);

    char auth_header[1024];
    snprintf(auth_header, sizeof(auth_header), "Bearer %s", token);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_UNAUTHORIZED,
                           auth_require_external_client(d, auth_header, TEST_EXTERNAL_CLIENT_ID));

    free(token);
    dispatcher_destroy(d);
}

/* ローカルRSA発行(iss=bff-gin-local-rsa)のトークンも同じ理由で拒否される
 * (HMAC/RSAどちらのローカル発行方式でも外部公開APIは受け付けない) */
static void test_rejects_local_rsa_issuer_even_with_correct_azp(void) {
    Dispatcher *d = build_test_dispatcher();
    char *token = make_rsa_token_with_azp(g_keypair, TEST_KID, AUTH_LOCAL_RSA_ISSUER, TEST_AUD, "1",
                                           TEST_EXTERNAL_CLIENT_ID, 3600);
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
    RUN_TEST(test_rejects_missing_authorization_header);
    RUN_TEST(test_rejects_unknown_issuer);
    RUN_TEST(test_accepts_valid_keycloak_client_credentials_token);
    RUN_TEST(test_rejects_wrong_azp);
    RUN_TEST(test_rejects_missing_azp);
    RUN_TEST(test_rejects_local_hmac_issuer_even_with_correct_azp);
    RUN_TEST(test_rejects_local_rsa_issuer_even_with_correct_azp);
    int result = UNITY_END();

    mg_stop(g_mock_ctx);
    mg_exit_library();
    EVP_PKEY_free(g_keypair);
    free(g_jwks_body);
    return result;
}
