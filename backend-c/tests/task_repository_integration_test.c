/*
 * docker-compose上の実MySQLに接続する結合テスト(backend-rust/tests/integration_test.rsと
 * 同じ考え方: HTTP層を経由せずRepository層を直接呼び出し、実際のデータベースで検証する)。
 *
 * backend_c_tests(Unity、DB接続不要な純粋な単体テストのみ)とは別の実行ファイルにしており、
 * CMakeのadd_test()には登録していない(`ctest`実行時に毎回docker composeを要求しないため)。
 * 実行するには事前に `docker compose up -d --wait mysql` してから:
 *
 *   cmake --build build -j 4
 *   DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development \
 *     ./build/backend_c_integration_tests
 *
 * テストごとに一意なemail/keycloak_sub/label名でユーザーとラベルを作り、
 * setUp/tearDownで確実に後始末する(共有の開発用DBを汚さないため、UnityのtearDownは
 * TEST_ASSERT失敗によるlongjmp後も呼ばれるので、アサート失敗時でも後始末される)
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "unity.h"

#include "common/error.h"
#include "db/mysql_conn.h"
#include "domain/task.h"
#include "repository/task_repository.h"

static const char *env_or(const char *name, const char *fallback) {
    const char *v = getenv(name);
    return v != NULL ? v : fallback;
}

/* このプロセス内で一意な値を作る(時刻+カウンタ、backend-rustのunique_suffixと同じ狙い) */
static long long unique_suffix(void) {
    static long long counter = 0;
    counter += 1;
    return (long long)time(NULL) * 1000000LL + (long long)getpid() % 100000LL * 100LL + counter;
}

static void run_sql_or_fail(const char *sql) {
    MYSQL *conn = mysql_conn_get();
    if (mysql_query(conn, sql) != 0) {
        fprintf(stderr, "integration test setup SQL failed: %s\nerror: %s\n", sql,
                mysql_error(conn));
        TEST_FAIL_MESSAGE("結合テストのセットアップSQLに失敗しました(docker compose up -d --wait mysqlは実行済みですか?)");
    }
}

static int64_t last_insert_id(void) {
    return (int64_t)mysql_insert_id(mysql_conn_get());
}

static int64_t query_scalar_i64(const char *sql) {
    MYSQL *conn = mysql_conn_get();
    if (mysql_query(conn, sql) != 0) {
        TEST_FAIL_MESSAGE("結合テストの検証SQLに失敗しました");
    }
    MYSQL_RES *res = mysql_store_result(conn);
    if (res == NULL) {
        TEST_FAIL_MESSAGE("結合テストの検証SQLの結果取得に失敗しました");
    }
    MYSQL_ROW row = mysql_fetch_row(res);
    int64_t value = (row != NULL && row[0] != NULL) ? atoll(row[0]) : -1;
    mysql_free_result(res);
    return value;
}

/* --- テストごとの後始末対象(setUpで採番し、tearDownで確実に削除する) --- */
static int64_t g_test_user_id;
static int64_t g_test_label_id;

void setUp(void) {
    long long suffix = unique_suffix();
    char sql[512];

    /* 【backend-rustとの突き合わせで判明した既知の差異】keycloak_subはusersテーブルではなく
     * 別テーブルuser_keycloaksに持つ(migration 000008_split_user_credentials)ため、
     * backend-rust/tests/integration_test.rsのcreate_test_userと同じく2テーブルへ分けて挿入する */
    snprintf(sql, sizeof(sql),
             "INSERT INTO users (email, name, role, created_at, updated_at) "
             "VALUES ('integration-c-%lld@example.com', 'Integration C Test User', 1, NOW(), NOW())",
             suffix);
    run_sql_or_fail(sql);
    g_test_user_id = last_insert_id();

    snprintf(sql, sizeof(sql),
             "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) "
             "VALUES (%lld, 'integration-c-kc-%lld', NOW(), NOW())",
             (long long)g_test_user_id, suffix);
    run_sql_or_fail(sql);

    snprintf(sql, sizeof(sql),
             "INSERT INTO labels (name, created_at, updated_at) VALUES ('integration-c-label-%lld', NOW(), NOW())",
             suffix);
    run_sql_or_fail(sql);
    g_test_label_id = last_insert_id();
}

void tearDown(void) {
    char sql[512];
    /* 外部キー制約は無いが、削除順序はtask_labels→tasks→labels/usersにしておく
     * (どの順でも制約違反にはならないが、他backendの慣例に合わせる) */
    snprintf(sql, sizeof(sql), "DELETE FROM task_labels WHERE label_id = %lld", (long long)g_test_label_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM tasks WHERE user_id = %lld", (long long)g_test_user_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM labels WHERE id = %lld", (long long)g_test_label_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM user_keycloaks WHERE user_id = %lld", (long long)g_test_user_id);
    run_sql_or_fail(sql);
    snprintf(sql, sizeof(sql), "DELETE FROM users WHERE id = %lld", (long long)g_test_user_id);
    run_sql_or_fail(sql);
}

static TaskInput *make_valid_input(const char *name) {
    TaskInput *input = task_input_create();
    TEST_ASSERT_NOT_NULL(input);
    TEST_ASSERT_EQUAL_INT(0, task_input_set_name(input, name));
    snprintf(input->status_raw, sizeof(input->status_raw), "%s", "waiting");
    snprintf(input->finished_on, sizeof(input->finished_on), "%s", "2999-01-01");
    return input;
}

/* create→find_by_id→update→delete の一連が実MySQL上で正しく動くことの確認 */
static void test_create_find_update_delete_round_trip(void) {
    TaskInput *input = make_valid_input("integration create");
    TEST_ASSERT_EQUAL_INT(0, task_input_add_label_id(input, g_test_label_id));

    int64_t task_id = 0;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_create(g_test_user_id, input, &task_id));
    task_input_destroy(input);
    TEST_ASSERT_TRUE(task_id > 0);

    Task *task = NULL;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_find_by_id(task_id, g_test_user_id, &task));
    TEST_ASSERT_EQUAL_STRING("integration create", task->name);
    TEST_ASSERT_EQUAL_UINT(1, task->label_count);
    TEST_ASSERT_TRUE(task->labels[0].id == g_test_label_id);
    task_destroy(task);

    TaskInput *update_input = make_valid_input("integration updated");
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_update(task_id, g_test_user_id, update_input));
    task_input_destroy(update_input);

    Task *updated = NULL;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_find_by_id(task_id, g_test_user_id, &updated));
    TEST_ASSERT_EQUAL_STRING("integration updated", updated->name);
    /* label_idsを渡さないupdateなのでreplace_labelsにより既存のラベル付けは解除される */
    TEST_ASSERT_EQUAL_UINT(0, updated->label_count);
    task_destroy(updated);

    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_delete(task_id, g_test_user_id));

    Task *after_delete = NULL;
    TEST_ASSERT_EQUAL_INT(TASK_ERR_NOT_FOUND,
                           task_repository_find_by_id(task_id, g_test_user_id, &after_delete));
}

/* 【backend-rustで見つかった既知バグの回帰防止】deleteがtasksとtask_labelsの両方を
 * トランザクションで削除し、孤立行を残さないことを実際のテーブルに対して確認する
 * (src/repository/task_repository.cのtask_repository_delete、README.md「削除のトランザクション保護」参照) */
static void test_delete_removes_task_labels_rows(void) {
    TaskInput *input = make_valid_input("int delete labels");
    TEST_ASSERT_EQUAL_INT(0, task_input_add_label_id(input, g_test_label_id));

    int64_t task_id = 0;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_create(g_test_user_id, input, &task_id));
    task_input_destroy(input);

    char count_sql[256];
    snprintf(count_sql, sizeof(count_sql), "SELECT COUNT(*) FROM task_labels WHERE task_id = %lld",
             (long long)task_id);
    TEST_ASSERT_EQUAL_INT(1, (int)query_scalar_i64(count_sql));

    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_delete(task_id, g_test_user_id));

    /* tasks行が消えた後もtask_labels側に孤立行が残っていないことを直接SQLで確認する */
    TEST_ASSERT_EQUAL_INT(0, (int)query_scalar_i64(count_sql));
}

/* 重複label_id([label_id, label_id])が1件に正規化されることの確認(replace_labelsのO(n^2)重複排除) */
static void test_create_dedups_duplicate_label_ids(void) {
    TaskInput *input = make_valid_input("integration dedup");
    TEST_ASSERT_EQUAL_INT(0, task_input_add_label_id(input, g_test_label_id));
    TEST_ASSERT_EQUAL_INT(0, task_input_add_label_id(input, g_test_label_id));

    int64_t task_id = 0;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_create(g_test_user_id, input, &task_id));
    task_input_destroy(input);

    char count_sql[256];
    snprintf(count_sql, sizeof(count_sql), "SELECT COUNT(*) FROM task_labels WHERE task_id = %lld",
             (long long)task_id);
    TEST_ASSERT_EQUAL_INT(1, (int)query_scalar_i64(count_sql));

    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_delete(task_id, g_test_user_id));
}

/* 他ユーザーのtaskはfind/update/deleteのいずれも見えない(所有権分離)ことの確認 */
static void test_other_user_cannot_see_task(void) {
    TaskInput *input = make_valid_input("int owner check");
    int64_t task_id = 0;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_create(g_test_user_id, input, &task_id));
    task_input_destroy(input);

    int64_t other_user_id = g_test_user_id + 1000000; /* 存在しないuser_id */
    Task *task = NULL;
    TEST_ASSERT_EQUAL_INT(TASK_ERR_NOT_FOUND,
                           task_repository_find_by_id(task_id, other_user_id, &task));
    TEST_ASSERT_EQUAL_INT(TASK_ERR_NOT_FOUND, task_repository_delete(task_id, other_user_id));

    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_delete(task_id, g_test_user_id));
}

/* JWT認証(auth/user_resolver.h)のuser_id解決に使うtask_repository_find_user_by_idの確認。
 * setUpで作った実在ユーザーは見つかり、存在しないidは見つからないことを両方確認する */
static void test_find_user_by_id(void) {
    bool found = false;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_find_user_by_id(g_test_user_id, &found));
    TEST_ASSERT_TRUE(found);

    bool not_found = true;
    int64_t nonexistent_id = g_test_user_id + 1000000;
    TEST_ASSERT_EQUAL_INT(TASK_OK, task_repository_find_user_by_id(nonexistent_id, &not_found));
    TEST_ASSERT_FALSE(not_found);
}

/* 【backend-rustとの突き合わせで判明した既知の差異(setUpのコメント参照)】keycloak_subは
 * usersテーブルではなくuser_keycloaksテーブルに分離されているため、JOIN経由で正しく
 * users.idを引けることを確認する(task_repository_find_user_by_keycloak_sub) */
static void test_find_user_by_keycloak_sub(void) {
    char sql[512];
    snprintf(sql, sizeof(sql), "SELECT keycloak_sub FROM user_keycloaks WHERE user_id = %lld",
             (long long)g_test_user_id);
    MYSQL *conn = mysql_conn_get();
    TEST_ASSERT_EQUAL_INT(0, mysql_query(conn, sql));
    MYSQL_RES *res = mysql_store_result(conn);
    TEST_ASSERT_NOT_NULL(res);
    MYSQL_ROW row = mysql_fetch_row(res);
    TEST_ASSERT_NOT_NULL(row);
    char keycloak_sub[256];
    snprintf(keycloak_sub, sizeof(keycloak_sub), "%s", row[0]);
    mysql_free_result(res);

    int64_t found_id = 0;
    bool found = false;
    TEST_ASSERT_EQUAL_INT(TASK_OK,
                           task_repository_find_user_by_keycloak_sub(keycloak_sub, &found_id, &found));
    TEST_ASSERT_TRUE(found);
    TEST_ASSERT_EQUAL_INT64(g_test_user_id, found_id);

    bool not_found = true;
    int64_t unused_id = 0;
    TEST_ASSERT_EQUAL_INT(
        TASK_OK, task_repository_find_user_by_keycloak_sub("nonexistent-keycloak-sub", &unused_id,
                                                             &not_found));
    TEST_ASSERT_FALSE(not_found);
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

    UNITY_BEGIN();
    RUN_TEST(test_create_find_update_delete_round_trip);
    RUN_TEST(test_delete_removes_task_labels_rows);
    RUN_TEST(test_create_dedups_duplicate_label_ids);
    RUN_TEST(test_other_user_cannot_see_task);
    RUN_TEST(test_find_user_by_id);
    RUN_TEST(test_find_user_by_keycloak_sub);
    return UNITY_END();
}
