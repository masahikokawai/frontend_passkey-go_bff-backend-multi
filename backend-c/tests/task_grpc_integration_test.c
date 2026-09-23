/*
 * gRPC v2の結合テスト。src/grpc/grpc_server.c(サーバー)をこのテストプロセス自身の中で
 * 実際に起動し、tests/grpc_test_client.c(自作の同期クライアント、サーバーと同じgRPC Core
 * C APIを直接使う)から実RPCを送って実DB(docker-compose上のMySQL)まで通す
 * (backend_c_integration_test.c=Repository層直呼び出しのDB結合テストとは別に、
 * gRPCのワイヤーまで含めて検証する。README.md「gRPC」節参照)。
 *
 * backend_c_tests(ctest対象、DB接続不要)とは別の実行ファイルであり、CMakeの
 * add_test()には登録していない。実行するには事前に `docker compose up -d --wait mysql`
 * してから:
 *
 *   cmake --build build -j 4
 *   DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development \
 *     ./build/backend_c_grpc_integration_tests
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "unity.h"

#include "auth/auth_module.h"
#include "auth/jwt.h"
#include "common/error.h"
#include "db/mysql_conn.h"
#include "grpc/grpc_server.h"
#include "grpc_test_client.h"
#include "task/v1/task.pb-c.h"
#include "test_token_helper.h"

/* このテストプロセス専用のローカルHMACシークレット(サーバー側main()の既定値とは無関係、
 * このプロセス内で自前のauth_module_initを呼ぶため一致させれば何でもよい) */
#define TEST_HMAC_SECRET "grpc-integration-test-hmac-secret"
#define TEST_AUDIENCE "backend"

static const char *env_or(const char *name, const char *fallback) {
    const char *v = getenv(name);
    return v != NULL ? v : fallback;
}

/* setUpで作ったg_test_user_idに対応する、実際に検証可能なHS256トークンを組み立てる
 * (呼び出し側がfree()すること)。ローカルHMAC発行者のsubは内部user_idそのもの
 * (auth/user_resolver.cの分岐、README.md「認証について」参照) */
static char *bearer_token_for_user(int64_t user_id) {
    char sub[32];
    snprintf(sub, sizeof(sub), "%lld", (long long)user_id);
    return make_hmac_token(TEST_HMAC_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUDIENCE, sub, 3600);
}

static long long unique_suffix(void) {
    static long long counter = 0;
    counter += 1;
    return (long long)time(NULL) * 1000000LL + (long long)getpid() % 100000LL * 100LL + counter;
}

static void run_sql_or_fail(const char *sql) {
    MYSQL *conn = mysql_conn_get();
    if (mysql_query(conn, sql) != 0) {
        fprintf(stderr, "grpc integration test setup SQL failed: %s\nerror: %s\n", sql,
                mysql_error(conn));
        TEST_FAIL_MESSAGE("結合テストのセットアップSQLに失敗しました(docker compose up -d --wait mysqlは実行済みですか?)");
    }
}

static int64_t last_insert_id(void) { return (int64_t)mysql_insert_id(mysql_conn_get()); }

static int64_t query_scalar_i64(const char *sql) {
    MYSQL *conn = mysql_conn_get();
    if (mysql_query(conn, sql) != 0) TEST_FAIL_MESSAGE("結合テストの検証SQLに失敗しました");
    MYSQL_RES *res = mysql_store_result(conn);
    if (res == NULL) TEST_FAIL_MESSAGE("結合テストの検証SQLの結果取得に失敗しました");
    MYSQL_ROW row = mysql_fetch_row(res);
    int64_t value = (row != NULL && row[0] != NULL) ? atoll(row[0]) : -1;
    mysql_free_result(res);
    return value;
}

static int64_t g_test_user_id;
static int64_t g_test_label_id;
static GrpcTestClient g_client;

#define TEST_GRPC_PORT_ENV "GRPC_TEST_PORT"
#define TEST_GRPC_DEFAULT_PORT "19100"

void setUp(void) {
    long long suffix = unique_suffix();
    char sql[512];

    snprintf(sql, sizeof(sql),
             "INSERT INTO users (email, name, role, created_at, updated_at) "
             "VALUES ('integration-c-grpc-%lld@example.com', 'Integration C gRPC Test User', 1, "
             "NOW(), NOW())",
             suffix);
    run_sql_or_fail(sql);
    g_test_user_id = last_insert_id();

    snprintf(sql, sizeof(sql),
             "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) "
             "VALUES (%lld, 'integration-c-grpc-kc-%lld', NOW(), NOW())",
             (long long)g_test_user_id, suffix);
    run_sql_or_fail(sql);

    snprintf(sql, sizeof(sql),
             "INSERT INTO labels (name, created_at, updated_at) VALUES "
             "('integration-c-grpc-label-%lld', NOW(), NOW())",
             suffix);
    run_sql_or_fail(sql);
    g_test_label_id = last_insert_id();
}

void tearDown(void) {
    char sql[512];
    snprintf(sql, sizeof(sql), "DELETE FROM task_labels WHERE label_id = %lld",
             (long long)g_test_label_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM tasks WHERE user_id = %lld", (long long)g_test_user_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM labels WHERE id = %lld", (long long)g_test_label_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM user_keycloaks WHERE user_id = %lld",
             (long long)g_test_user_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM users WHERE id = %lld", (long long)g_test_user_id);
    run_sql_or_fail(sql);
}

/* --- 各RPCのpack/call/unpackをまとめる薄いヘルパー群 --- */

static grpc_status_code call_create_task(int64_t user_id, const char *name,
                                          const char *finished_on, Task__V1__Task **out_task) {
    Task__V1__CreateTaskRequest req = TASK__V1__CREATE_TASK_REQUEST__INIT;
    req.name = (char *)name;
    req.status = (char *)"waiting";
    req.finished_on = (char *)finished_on;

    size_t size = task__v1__create_task_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__create_task_request__pack(&req, buf);

    char *token = bearer_token_for_user(user_id);
    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/CreateTask", token, buf, size, &result);
    free(token);
    free(buf);

    grpc_status_code status = result.status;
    if (status == GRPC_STATUS_OK && out_task != NULL) {
        *out_task = task__v1__task__unpack(NULL, result.response_len, result.response_bytes);
    }
    grpc_test_call_result_destroy(&result);
    return status;
}

static grpc_status_code call_get_task(int64_t user_id, int64_t task_id, Task__V1__Task **out_task) {
    Task__V1__GetTaskRequest req = TASK__V1__GET_TASK_REQUEST__INIT;
    req.id = (uint64_t)task_id;
    size_t size = task__v1__get_task_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__get_task_request__pack(&req, buf);

    char *token = bearer_token_for_user(user_id);
    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/GetTask", token, buf, size, &result);
    free(token);
    free(buf);

    grpc_status_code status = result.status;
    if (status == GRPC_STATUS_OK && out_task != NULL) {
        *out_task = task__v1__task__unpack(NULL, result.response_len, result.response_bytes);
    }
    grpc_test_call_result_destroy(&result);
    return status;
}

static grpc_status_code call_update_task(int64_t user_id, int64_t task_id, const char *name,
                                          const char *finished_on, Task__V1__Task **out_task) {
    Task__V1__UpdateTaskRequest req = TASK__V1__UPDATE_TASK_REQUEST__INIT;
    req.id = (uint64_t)task_id;
    req.name = (char *)name;
    req.status = (char *)"work_in_progress";
    req.finished_on = (char *)finished_on;

    size_t size = task__v1__update_task_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__update_task_request__pack(&req, buf);

    char *token = bearer_token_for_user(user_id);
    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/UpdateTask", token, buf, size, &result);
    free(token);
    free(buf);

    grpc_status_code status = result.status;
    if (status == GRPC_STATUS_OK && out_task != NULL) {
        *out_task = task__v1__task__unpack(NULL, result.response_len, result.response_bytes);
    }
    grpc_test_call_result_destroy(&result);
    return status;
}

static grpc_status_code call_delete_task(int64_t user_id, int64_t task_id) {
    Task__V1__DeleteTaskRequest req = TASK__V1__DELETE_TASK_REQUEST__INIT;
    req.id = (uint64_t)task_id;
    size_t size = task__v1__delete_task_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__delete_task_request__pack(&req, buf);

    char *token = bearer_token_for_user(user_id);
    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/DeleteTask", token, buf, size, &result);
    free(token);
    free(buf);
    grpc_status_code status = result.status;
    grpc_test_call_result_destroy(&result);
    return status;
}

static grpc_status_code call_list_tasks(int64_t user_id, uint64_t cursor, int32_t limit,
                                         Task__V1__ListTasksResponse **out_resp) {
    Task__V1__ListTasksRequest req = TASK__V1__LIST_TASKS_REQUEST__INIT;
    req.cursor = cursor;
    req.limit = limit;
    size_t size = task__v1__list_tasks_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__list_tasks_request__pack(&req, buf);

    char *token = bearer_token_for_user(user_id);
    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/ListTasks", token, buf, size, &result);
    free(token);
    free(buf);

    grpc_status_code status = result.status;
    if (status == GRPC_STATUS_OK && out_resp != NULL) {
        *out_resp = task__v1__list_tasks_response__unpack(NULL, result.response_len,
                                                            result.response_bytes);
    }
    grpc_test_call_result_destroy(&result);
    return status;
}

/* --- テスト本体 --- */

static void test_grpc_create_get_update_delete_round_trip(void) {
    Task__V1__Task *created = NULL;
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK,
                           call_create_task(g_test_user_id, "grpc create", "2999-01-01", &created));
    TEST_ASSERT_NOT_NULL(created);
    TEST_ASSERT_EQUAL_STRING("grpc create", created->name);
    int64_t task_id = (int64_t)created->id;
    task__v1__task__free_unpacked(created, NULL);

    Task__V1__Task *fetched = NULL;
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK, call_get_task(g_test_user_id, task_id, &fetched));
    TEST_ASSERT_EQUAL_STRING("grpc create", fetched->name);
    task__v1__task__free_unpacked(fetched, NULL);

    Task__V1__Task *updated = NULL;
    TEST_ASSERT_EQUAL_INT(
        GRPC_STATUS_OK, call_update_task(g_test_user_id, task_id, "grpc updated", "2999-01-01", &updated));
    TEST_ASSERT_EQUAL_STRING("grpc updated", updated->name);
    TEST_ASSERT_EQUAL_STRING("work_in_progress", updated->status);
    task__v1__task__free_unpacked(updated, NULL);

    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK, call_delete_task(g_test_user_id, task_id));
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_NOT_FOUND, call_get_task(g_test_user_id, task_id, NULL));
}

/* 【backend-rustで見つかった既知バグの回帰防止、REST/gRPCどちらも同じRepositoryを共有する
 * ことの確認】gRPC経由のdeleteでもtasksとtask_labelsが1トランザクションで削除され、
 * 孤立行が残らないことを実際のテーブルに対して確認する */
static void test_grpc_delete_removes_task_labels_rows(void) {
    Task__V1__CreateTaskRequest req = TASK__V1__CREATE_TASK_REQUEST__INIT;
    req.name = (char *)"grpc delete labels";
    req.status = (char *)"waiting";
    req.finished_on = (char *)"2999-01-01";
    req.n_label_ids = 1;
    uint64_t label_ids[1] = {(uint64_t)g_test_label_id};
    req.label_ids = label_ids;

    size_t size = task__v1__create_task_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__create_task_request__pack(&req, buf);
    char *token = bearer_token_for_user(g_test_user_id);
    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/CreateTask", token, buf, size, &result);
    free(token);
    free(buf);
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK, result.status);
    Task__V1__Task *created =
        task__v1__task__unpack(NULL, result.response_len, result.response_bytes);
    grpc_test_call_result_destroy(&result);
    TEST_ASSERT_EQUAL_UINT(1, created->n_labels);
    int64_t task_id = (int64_t)created->id;
    task__v1__task__free_unpacked(created, NULL);

    char count_sql[256];
    snprintf(count_sql, sizeof(count_sql), "SELECT COUNT(*) FROM task_labels WHERE task_id = %lld",
             (long long)task_id);
    TEST_ASSERT_EQUAL_INT(1, (int)query_scalar_i64(count_sql));

    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK, call_delete_task(g_test_user_id, task_id));
    TEST_ASSERT_EQUAL_INT(0, (int)query_scalar_i64(count_sql));
}

/* authorizationメタデータを付けない呼び出しはUNAUTHENTICATEDになることの確認
 * (src/grpc/grpc_server.cのresolve_user_id_from_metadata) */
static void test_grpc_unauthenticated_call_rejected(void) {
    Task__V1__ListTasksRequest req = TASK__V1__LIST_TASKS_REQUEST__INIT;
    size_t size = task__v1__list_tasks_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__list_tasks_request__pack(&req, buf);

    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/ListTasks", /*bearer_token=*/NULL, buf,
                          size, &result);
    free(buf);
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_UNAUTHENTICATED, result.status);
    grpc_test_call_result_destroy(&result);
}

/* 期限切れトークンでの呼び出しもUNAUTHENTICATEDになることの確認(JWT本実装、
 * src/auth/jwt.cのjwt_claims_valid経由のexpチェック) */
static void test_grpc_expired_token_rejected(void) {
    Task__V1__ListTasksRequest req = TASK__V1__LIST_TASKS_REQUEST__INIT;
    size_t size = task__v1__list_tasks_request__get_packed_size(&req);
    uint8_t *buf = (uint8_t *)malloc(size);
    task__v1__list_tasks_request__pack(&req, buf);

    char sub[32];
    snprintf(sub, sizeof(sub), "%lld", (long long)g_test_user_id);
    char *expired_token =
        make_hmac_token(TEST_HMAC_SECRET, AUTH_LOCAL_HMAC_ISSUER, TEST_AUDIENCE, sub, -3600);

    GrpcTestCallResult result;
    grpc_test_client_call(&g_client, "/task.v1.TaskService/ListTasks", expired_token, buf, size,
                          &result);
    free(expired_token);
    free(buf);
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_UNAUTHENTICATED, result.status);
    grpc_test_call_result_destroy(&result);
}

/* idの昇順keyset cursorが正しく次ページへ進むことの確認(task_repository_list_cursor) */
static void test_grpc_list_tasks_cursor_pagination(void) {
    int64_t ids[3];
    for (int i = 0; i < 3; i++) {
        Task__V1__Task *created = NULL;
        char name[32];
        snprintf(name, sizeof(name), "grpc list %d", i);
        TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK,
                               call_create_task(g_test_user_id, name, "2999-01-01", &created));
        ids[i] = (int64_t)created->id;
        task__v1__task__free_unpacked(created, NULL);
    }

    Task__V1__ListTasksResponse *page1 = NULL;
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK, call_list_tasks(g_test_user_id, 0, 2, &page1));
    TEST_ASSERT_EQUAL_UINT(2, page1->n_tasks);
    TEST_ASSERT_TRUE(page1->next_cursor != 0);
    uint64_t cursor = page1->next_cursor;
    task__v1__list_tasks_response__free_unpacked(page1, NULL);

    Task__V1__ListTasksResponse *page2 = NULL;
    TEST_ASSERT_EQUAL_INT(GRPC_STATUS_OK, call_list_tasks(g_test_user_id, cursor, 2, &page2));
    TEST_ASSERT_EQUAL_UINT(1, page2->n_tasks);
    TEST_ASSERT_EQUAL_UINT(0, page2->next_cursor); /* 次ページ無し */
    task__v1__list_tasks_response__free_unpacked(page2, NULL);

    for (int i = 0; i < 3; i++) call_delete_task(g_test_user_id, ids[i]);
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

    /* JWT本実装(auth/auth_module.h)。gRPCサーバー(src/grpc/grpc_server.c)は
     * auth_module_dispatcher()経由でこのDispatcherを参照するため、grpc_server_module_start
     * より前に初期化しておく必要がある。Keycloak/ローカルRSAのJWKS URLはこのテストでは
     * 実際に使わない(HMAC経路のみを検証する、jwks_test.cがJWKS経路を別途カバーする)ため、
     * 到達不能なダミーURLのままで構わない */
    if (auth_module_init("https://unused-keycloak-issuer.invalid",
                          "https://unused-keycloak-issuer.invalid/protocol/openid-connect/certs",
                          TEST_AUDIENCE, TEST_HMAC_SECRET,
                          "https://unused-local-rsa-jwks.invalid", "external-api-client") != 0) {
        fprintf(stderr, "auth_module_init failed\n");
        return 1;
    }

    const char *port = env_or(TEST_GRPC_PORT_ENV, TEST_GRPC_DEFAULT_PORT);
    char listen_addr[64];
    snprintf(listen_addr, sizeof(listen_addr), "0.0.0.0:%s", port);
    if (grpc_server_module_start(listen_addr) != 0) {
        fprintf(stderr, "grpc_server_module_start failed (port %s in use?)\n", port);
        return 1;
    }

    char target[64];
    snprintf(target, sizeof(target), "127.0.0.1:%s", port);
    if (grpc_test_client_init(&g_client, target) != 0) {
        fprintf(stderr, "grpc_test_client_init failed\n");
        return 1;
    }

    UNITY_BEGIN();
    RUN_TEST(test_grpc_create_get_update_delete_round_trip);
    RUN_TEST(test_grpc_delete_removes_task_labels_rows);
    RUN_TEST(test_grpc_unauthenticated_call_rejected);
    RUN_TEST(test_grpc_expired_token_rejected);
    RUN_TEST(test_grpc_list_tasks_cursor_pagination);
    int result = UNITY_END();

    grpc_test_client_destroy(&g_client);
    grpc_server_module_stop();
    return result;
}
