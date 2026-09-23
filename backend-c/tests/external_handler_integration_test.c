/*
 * 外部公開API(src/external/external_handler.c)の結合テスト。実DB(docker-compose上の
 * MySQL)に加えて、このテストプロセス自身の中に「モックJWKSサーバー」(jwks_test.cと同じ
 * 手法、Keycloak相当として扱う)と「外部公開API自体のCivetWebリスナー」の両方を立てて、
 * libcurlで実際にHTTP GETを送り、認証・ページング(v1 offset/v2 cursor)をエンドツーエンドで
 * 確認する。
 *
 * backend.external-tasks-pagination-v2は全言語で共有する1つのFeature Flagのため、
 * setUp/tearDownで必ず元の値へ復元する(他のテスト実行・実機動作に影響を残さない。
 * UnityのtearDownはTEST_ASSERT失敗によるlongjmp後も呼ばれるため、アサート失敗時でも復元される)
 *
 * 実行方法はREADME.md「結合テスト」節を参照(docker compose up -d --wait mysql後、
 * DB_HOST等の環境変数付きで直接実行する)
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include <civetweb.h>
#include <cjson/cJSON.h>
#include <curl/curl.h>
#include <openssl/bn.h>
#include <openssl/evp.h>
#include <openssl/rsa.h>

#include "unity.h"

#include "auth/auth_module.h"
#include "auth/base64url.h"
#include "common/error.h"
#include "db/mysql_conn.h"
#include "domain/task.h"
#include "external/external_handler.h"
#include "flags/feature_flag_poller.h"
#include "repository/task_repository.h"
#include "test_token_helper.h"

#define MOCK_KID "external-test-kid"
#define MOCK_ISS "https://mock-keycloak.example.test"
#define MOCK_AUD "backend"
#define EXTERNAL_CLIENT_ID "external-api-client"
#define PAGINATION_FLAG_KEY "backend.external-tasks-pagination-v2"

static const char *env_or(const char *name, const char *fallback) {
    const char *v = getenv(name);
    return v != NULL ? v : fallback;
}

static long long unique_suffix(void) {
    static long long counter = 0;
    counter += 1;
    return (long long)time(NULL) * 1000000LL + (long long)getpid() % 100000LL * 100LL + counter;
}

static void run_sql_or_fail(const char *sql) {
    MYSQL *conn = mysql_conn_get();
    if (mysql_query(conn, sql) != 0) {
        fprintf(stderr, "external_handler_integration_test setup SQL failed: %s\nerror: %s\n",
                sql, mysql_error(conn));
        TEST_FAIL_MESSAGE("結合テストのセットアップSQLに失敗しました(docker compose up -d --wait mysqlは実行済みですか?)");
    }
}

static int64_t last_insert_id(void) { return (int64_t)mysql_insert_id(mysql_conn_get()); }

/* --- モックJWKSサーバー(jwks_test.cと同じ手法) --- */
static EVP_PKEY *g_keypair = NULL;
static char *g_jwks_body = NULL;
static struct mg_context *g_jwks_ctx = NULL;
static int g_jwks_port = 0;

/* --- 外部公開API自身のCivetWebリスナー --- */
static struct mg_context *g_external_ctx = NULL;
static int g_external_port = 0;

/* --- テストごとの後始末対象 --- */
static int64_t g_test_user_id;

/* --- backend.external-tasks-pagination-v2の退避/復元 --- */
static int g_saved_flag_enabled;
static char g_saved_flag_variation[64];

static char *bignum_to_base64url(const BIGNUM *bn) {
    int len = BN_num_bytes(bn);
    uint8_t *buf = (uint8_t *)malloc((size_t)len > 0 ? (size_t)len : 1);
    BN_bn2bin(bn, buf);
    char *encoded = base64url_encode(buf, (size_t)len);
    free(buf);
    return encoded;
}

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
    cJSON_AddStringToObject(key, "kid", MOCK_KID);
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

/* --- libcurlで外部公開APIへ実際にHTTP GETする、成長バッファ(src/auth/jwks_verifier.cの
 * http_get_cbと同じ手法をテスト用に複製したもの) --- */
typedef struct {
    char *data;
    size_t len;
    size_t cap;
} CurlBuffer;

static size_t curl_write_cb(void *ptr, size_t size, size_t nmemb, void *userdata) {
    size_t add = size * nmemb;
    CurlBuffer *buf = (CurlBuffer *)userdata;
    if (buf->len + add + 1 > buf->cap) {
        size_t new_cap = (buf->len + add + 1) * 2;
        char *grown = (char *)realloc(buf->data, new_cap);
        if (grown == NULL) return 0;
        buf->data = grown;
        buf->cap = new_cap;
    }
    memcpy(buf->data + buf->len, ptr, add);
    buf->len += add;
    buf->data[buf->len] = '\0';
    return add;
}

/* path("/external/v1/tasks?...")へGETする。authorization_headerはNULL可
 * (未指定のAuthorizationヘッダをテストするため)。戻り値: HTTPステータス
 * (-1は接続自体の失敗)、*out_bodyへ呼び出し側がfree()すべきレスポンスボディを書く */
static long http_get(const char *path, const char *authorization_header, char **out_body) {
    char url[256];
    snprintf(url, sizeof(url), "http://127.0.0.1:%d%s", g_external_port, path);

    CURL *curl = curl_easy_init();
    if (curl == NULL) return -1;

    CurlBuffer buf = {0};
    buf.cap = 256;
    buf.data = (char *)malloc(buf.cap);
    buf.data[0] = '\0';

    struct curl_slist *headers = NULL;
    if (authorization_header != NULL) {
        char header_line[1200];
        snprintf(header_line, sizeof(header_line), "Authorization: %s", authorization_header);
        headers = curl_slist_append(headers, header_line);
    }

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curl_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &buf);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 5L);

    CURLcode res = curl_easy_perform(curl);
    long http_code = -1;
    if (res == CURLE_OK) {
        curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
    }
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);

    *out_body = buf.data;
    return http_code;
}

static char *make_keycloak_like_token(const char *sub, const char *azp) {
    return make_rsa_token_with_azp(g_keypair, MOCK_KID, MOCK_ISS, MOCK_AUD, sub, azp, 3600);
}

void setUp(void) {
    long long suffix = unique_suffix();
    char sql[512];
    snprintf(sql, sizeof(sql),
             "INSERT INTO users (email, name, role, created_at, updated_at) "
             "VALUES ('external-c-%lld@example.com', 'External C Test User', 1, NOW(), NOW())",
             suffix);
    run_sql_or_fail(sql);
    g_test_user_id = last_insert_id();

    /* 全言語で共有するFeature Flagを退避する(このテストファイル内でのみ書き換え、
     * tearDownで必ず元に戻す) */
    MYSQL *conn = mysql_conn_get();
    snprintf(sql, sizeof(sql),
             "SELECT enabled, default_variation FROM feature_flags WHERE flag_key = '%s'",
             PAGINATION_FLAG_KEY);
    if (mysql_query(conn, sql) != 0) TEST_FAIL_MESSAGE("Feature Flagの退避に失敗しました");
    MYSQL_RES *res = mysql_store_result(conn);
    MYSQL_ROW row = res != NULL ? mysql_fetch_row(res) : NULL;
    if (row == NULL) {
        if (res != NULL) mysql_free_result(res);
        TEST_FAIL_MESSAGE("backend.external-tasks-pagination-v2が見つかりません");
    }
    g_saved_flag_enabled = atoi(row[0]);
    snprintf(g_saved_flag_variation, sizeof(g_saved_flag_variation), "%s",
             row[1] != NULL ? row[1] : "off");
    mysql_free_result(res);
}

void tearDown(void) {
    char sql[512];
    snprintf(sql, sizeof(sql), "DELETE FROM tasks WHERE user_id = %lld", (long long)g_test_user_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM users WHERE id = %lld", (long long)g_test_user_id);
    run_sql_or_fail(sql);

    /* Feature Flagを元の値へ復元し、キャッシュも即座に反映させる(次のテスト・
     * 実機動作へ影響を残さないため) */
    snprintf(sql, sizeof(sql),
             "UPDATE feature_flags SET enabled = %d, default_variation = '%s' WHERE flag_key = "
             "'%s'",
             g_saved_flag_enabled, g_saved_flag_variation, PAGINATION_FLAG_KEY);
    run_sql_or_fail(sql);
    feature_flag_poller_poll_once_for_test();
}

static int64_t create_task(const char *name) {
    TaskInput *input = task_input_create();
    task_input_set_name(input, name);
    snprintf(input->status_raw, sizeof(input->status_raw), "%s", "waiting");
    snprintf(input->finished_on, sizeof(input->finished_on), "%s", "2999-01-01");
    int64_t task_id = 0;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_create(g_test_user_id, input, &task_id));
    task_input_destroy(input);
    return task_id;
}

static void set_pagination_flag(int enabled, const char *variation) {
    char sql[256];
    snprintf(sql, sizeof(sql),
             "UPDATE feature_flags SET enabled = %d, default_variation = '%s' WHERE flag_key = "
             "'%s'",
             enabled, variation, PAGINATION_FLAG_KEY);
    run_sql_or_fail(sql);
    feature_flag_poller_poll_once_for_test();
}

static void test_rejects_missing_authorization(void) {
    char path[128];
    snprintf(path, sizeof(path), "/external/v1/tasks?user_id=%lld", (long long)g_test_user_id);
    char *body = NULL;
    long status = http_get(path, NULL, &body);
    TEST_ASSERT_EQUAL_INT(401, (int)(status));
    TEST_ASSERT_NOT_NULL(strstr(body, "unauthenticated"));
    free(body);
}

static void test_rejects_wrong_azp(void) {
    char *token = make_keycloak_like_token("some-keycloak-sub", "someone-else-client");
    char auth[1024];
    snprintf(auth, sizeof(auth), "Bearer %s", token);

    char path[128];
    snprintf(path, sizeof(path), "/external/v1/tasks?user_id=%lld", (long long)g_test_user_id);
    char *body = NULL;
    long status = http_get(path, auth, &body);
    TEST_ASSERT_EQUAL_INT(401, (int)(status));

    free(token);
    free(body);
}

static void test_rejects_missing_user_id(void) {
    char *token = make_keycloak_like_token("some-keycloak-sub", EXTERNAL_CLIENT_ID);
    char auth[1024];
    snprintf(auth, sizeof(auth), "Bearer %s", token);

    char *body = NULL;
    long status = http_get("/external/v1/tasks", auth, &body);
    TEST_ASSERT_EQUAL_INT(400, (int)(status));
    TEST_ASSERT_NOT_NULL(strstr(body, "user_id_required"));

    free(token);
    free(body);
}

/* v1(既定、offsetページング)がページ境界をまたいで正しく動くことの確認
 * (task_repository_list、ORDER BY created_at DESC, id DESCのtie-break込み) */
static void test_offset_pagination_across_pages(void) {
    set_pagination_flag(0, "off"); /* v1を明示的に選ぶ */

    int64_t id1 = create_task("ext-offset-1");
    int64_t id2 = create_task("ext-offset-2");
    int64_t id3 = create_task("ext-offset-3");
    /* created_at DESC, id DESCなので、新しいid(id3)ほど先に出てくる */

    char *token = make_keycloak_like_token("some-keycloak-sub", EXTERNAL_CLIENT_ID);
    char auth[1024];
    snprintf(auth, sizeof(auth), "Bearer %s", token);

    char path[160];
    snprintf(path, sizeof(path), "/external/v1/tasks?user_id=%lld&page=1&page_size=2",
             (long long)g_test_user_id);
    char *body1 = NULL;
    TEST_ASSERT_EQUAL_INT(200, (int)(http_get(path, auth, &body1)));
    cJSON *json1 = cJSON_Parse(body1);
    TEST_ASSERT_NOT_NULL(json1);
    TEST_ASSERT_EQUAL_INT(2, cJSON_GetArraySize(cJSON_GetObjectItem(json1, "tasks")));
    TEST_ASSERT_EQUAL_INT(3, (int)cJSON_GetObjectItem(json1, "total")->valuedouble);
    TEST_ASSERT_EQUAL_INT(
        (int)id3,
        (int)cJSON_GetObjectItem(cJSON_GetArrayItem(cJSON_GetObjectItem(json1, "tasks"), 0), "id")
            ->valuedouble);
    TEST_ASSERT_EQUAL_INT(
        (int)id2,
        (int)cJSON_GetObjectItem(cJSON_GetArrayItem(cJSON_GetObjectItem(json1, "tasks"), 1), "id")
            ->valuedouble);
    cJSON_Delete(json1);
    free(body1);

    snprintf(path, sizeof(path), "/external/v1/tasks?user_id=%lld&page=2&page_size=2",
             (long long)g_test_user_id);
    char *body2 = NULL;
    TEST_ASSERT_EQUAL_INT(200, (int)(http_get(path, auth, &body2)));
    cJSON *json2 = cJSON_Parse(body2);
    TEST_ASSERT_NOT_NULL(json2);
    TEST_ASSERT_EQUAL_INT(1, cJSON_GetArraySize(cJSON_GetObjectItem(json2, "tasks")));
    TEST_ASSERT_EQUAL_INT(
        (int)id1,
        (int)cJSON_GetObjectItem(cJSON_GetArrayItem(cJSON_GetObjectItem(json2, "tasks"), 0), "id")
            ->valuedouble);
    cJSON_Delete(json2);
    free(body2);

    free(token);
}

/* v2(cursorページング)がnext_cursorを正しく連鎖させ、最終ページでnullになることの確認 */
static void test_cursor_pagination_chains_to_null(void) {
    set_pagination_flag(1, "on");

    int64_t id1 = create_task("ext-cursor-1");
    int64_t id2 = create_task("ext-cursor-2");
    int64_t id3 = create_task("ext-cursor-3");
    (void)id1;

    char *token = make_keycloak_like_token("some-keycloak-sub", EXTERNAL_CLIENT_ID);
    char auth[1024];
    snprintf(auth, sizeof(auth), "Bearer %s", token);

    /* task_repository_list_cursorはid昇順(古い順)、id1→id2→id3の順で返る */
    char path[160];
    snprintf(path, sizeof(path), "/external/v1/tasks?user_id=%lld&limit=2",
             (long long)g_test_user_id);
    char *body1 = NULL;
    TEST_ASSERT_EQUAL_INT(200, (int)(http_get(path, auth, &body1)));
    cJSON *json1 = cJSON_Parse(body1);
    TEST_ASSERT_EQUAL_INT(2, cJSON_GetArraySize(cJSON_GetObjectItem(json1, "tasks")));
    cJSON *next_cursor1 = cJSON_GetObjectItem(json1, "next_cursor");
    TEST_ASSERT_TRUE(cJSON_IsString(next_cursor1));
    char cursor1[32];
    snprintf(cursor1, sizeof(cursor1), "%s", next_cursor1->valuestring);
    cJSON_Delete(json1);
    free(body1);

    snprintf(path, sizeof(path), "/external/v1/tasks?user_id=%lld&cursor=%s&limit=2",
             (long long)g_test_user_id, cursor1);
    char *body2 = NULL;
    TEST_ASSERT_EQUAL_INT(200, (int)(http_get(path, auth, &body2)));
    cJSON *json2 = cJSON_Parse(body2);
    TEST_ASSERT_EQUAL_INT(1, cJSON_GetArraySize(cJSON_GetObjectItem(json2, "tasks")));
    TEST_ASSERT_EQUAL_INT(
        (int)id3,
        (int)cJSON_GetObjectItem(cJSON_GetArrayItem(cJSON_GetObjectItem(json2, "tasks"), 0), "id")
            ->valuedouble);
    TEST_ASSERT_TRUE(cJSON_IsNull(cJSON_GetObjectItem(json2, "next_cursor")));
    cJSON_Delete(json2);
    free(body2);

    free(token);
}

int main(void) {
    const char *db_host = env_or("DB_HOST", "127.0.0.1");
    unsigned int db_port = (unsigned int)atoi(env_or("DB_PORT", "13306"));
    const char *db_user = env_or("DB_USER", "root");
    const char *db_password = env_or("DB_PASSWORD", "");
    const char *db_schema = env_or("DB_SCHEMA", "bff_gin_development");

    if (mysql_conn_module_init(db_host, db_port, db_user, db_password, db_schema) != 0) {
        fprintf(stderr, "mysql_conn_module_init failed\n");
        return 1;
    }
    if (mysql_conn_get() == NULL) {
        fprintf(stderr,
                "MySQLへ接続できませんでした。docker compose up -d --wait mysql は実行済みですか?\n");
        return 1;
    }

    build_mock_jwks();

    mg_init_library(0);

    const char *jwks_options[] = {"listening_ports", "0", "num_threads", "4", NULL};
    g_jwks_ctx = mg_start(NULL, NULL, jwks_options);
    if (g_jwks_ctx == NULL) {
        fprintf(stderr, "mock jwks server mg_start failed\n");
        return 1;
    }
    mg_set_request_handler(g_jwks_ctx, "/jwks", jwks_handler, NULL);
    struct mg_server_ports jwks_port_list[8];
    int jn = mg_get_server_ports(g_jwks_ctx, 8, jwks_port_list);
    g_jwks_port = (jn > 0) ? jwks_port_list[0].port : 0;
    if (g_jwks_port == 0) {
        fprintf(stderr, "failed to determine mock jwks server port\n");
        return 1;
    }

    char jwks_url[128];
    snprintf(jwks_url, sizeof(jwks_url), "http://127.0.0.1:%d/jwks", g_jwks_port);
    /* MOCK_ISSを「Keycloak」として登録する(ローカルRSA/HMACのURL・秘密鍵はこのテストでは
     * 使わないため到達不能なダミー値のままでよい) */
    if (auth_module_init(MOCK_ISS, jwks_url, MOCK_AUD, "unused-local-hmac-secret",
                          "https://unused-local-rsa-jwks.invalid", EXTERNAL_CLIENT_ID) != 0) {
        fprintf(stderr, "auth_module_init failed\n");
        return 1;
    }

    const char *external_options[] = {"listening_ports", "0", "num_threads", "8", NULL};
    g_external_ctx = mg_start(NULL, NULL, external_options);
    if (g_external_ctx == NULL) {
        fprintf(stderr, "external listener mg_start failed\n");
        return 1;
    }
    mg_set_request_handler(g_external_ctx, "/external/v1/tasks", external_task_request_handler,
                            NULL);
    struct mg_server_ports ext_port_list[8];
    int en = mg_get_server_ports(g_external_ctx, 8, ext_port_list);
    g_external_port = (en > 0) ? ext_port_list[0].port : 0;
    if (g_external_port == 0) {
        fprintf(stderr, "failed to determine external listener port\n");
        return 1;
    }

    UNITY_BEGIN();
    RUN_TEST(test_rejects_missing_authorization);
    RUN_TEST(test_rejects_wrong_azp);
    RUN_TEST(test_rejects_missing_user_id);
    RUN_TEST(test_offset_pagination_across_pages);
    RUN_TEST(test_cursor_pagination_chains_to_null);
    int result = UNITY_END();

    mg_stop(g_external_ctx);
    mg_stop(g_jwks_ctx);
    mg_exit_library();
    EVP_PKEY_free(g_keypair);
    free(g_jwks_body);
    return result;
}
