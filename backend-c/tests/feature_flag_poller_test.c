/*
 * Feature Flagポーリング(src/flags/feature_flag_poller.c)の結合テスト。
 * 実DBの`feature_flags`テーブルへ実際にテスト専用の行を挿入し、
 * feature_flag_poller_poll_once_for_test()(10秒間隔を待たない即時ポーリング)経由で
 * Variation()のフォールバック規則(enabled=false/未登録ならdefault_value、
 * enabled=trueならdefault_variation)を確認する。
 *
 * backend.task-language等、他の機能が実際に参照している既存のflag行には一切触れず、
 * このテスト専用の一意なflag_keyを使う(共有の開発用DBを汚さない、他のテスト実行と
 * 競合しないため。tests/task_repository_integration_test.cと同じ「テストごとに一意な
 * 値を使う」方針)
 *
 * 実行方法はREADME.md「結合テスト」節を参照(docker compose up -d --wait mysql後、
 * DB_HOST等の環境変数付きで直接実行する)
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "unity.h"

#include "db/mysql_conn.h"
#include "flags/feature_flag_poller.h"

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
        fprintf(stderr, "feature_flag_poller_test setup SQL failed: %s\nerror: %s\n", sql,
                mysql_error(conn));
        TEST_FAIL_MESSAGE("結合テストのセットアップSQLに失敗しました(docker compose up -d --wait mysqlは実行済みですか?)");
    }
}

static char g_test_flag_key[128];

void setUp(void) {
    long long suffix = unique_suffix();
    snprintf(g_test_flag_key, sizeof(g_test_flag_key), "test.feature-flag-poller-%lld", suffix);
}

void tearDown(void) {
    char sql[256];
    snprintf(sql, sizeof(sql), "DELETE FROM feature_flags WHERE flag_key = '%s'",
             g_test_flag_key);
    /* run_sql_or_failだとDELETE自体の失敗でテスト結果を隠してしまうため、後始末の
     * 失敗はログのみに留める(他の結合テストのtearDownと同じ「assert失敗後でも
     * 後始末を試みる」方針) */
    MYSQL *conn = mysql_conn_get();
    if (mysql_query(conn, sql) != 0) {
        fprintf(stderr, "feature_flag_poller_test cleanup failed: %s\n", mysql_error(conn));
    }
}

/* enabled=1の行はdefault_variationがそのまま返る */
static void test_variation_returns_default_variation_when_enabled(void) {
    char sql[512];
    snprintf(sql, sizeof(sql),
             "INSERT INTO feature_flags (flag_key, description, enabled, default_variation, "
             "variations, created_at, updated_at) VALUES ('%s', 'poller test', 1, 'seeded-on', "
             "JSON_OBJECT('seeded-on', 'seeded-on', 'off', 'off'), NOW(), NOW())",
             g_test_flag_key);
    run_sql_or_fail(sql);

    feature_flag_poller_poll_once_for_test();

    char out[64];
    feature_flag_poller_variation(g_test_flag_key, "fallback", out, sizeof(out));
    TEST_ASSERT_EQUAL_STRING("seeded-on", out);
}

/* enabled=0の行はdefault_variationを無視し、呼び出し側が渡したdefault_valueが返る */
static void test_variation_returns_fallback_when_disabled(void) {
    char sql[512];
    snprintf(sql, sizeof(sql),
             "INSERT INTO feature_flags (flag_key, description, enabled, default_variation, "
             "variations, created_at, updated_at) VALUES ('%s', 'poller test', 0, 'seeded-on', "
             "JSON_OBJECT('seeded-on', 'seeded-on', 'off', 'off'), NOW(), NOW())",
             g_test_flag_key);
    run_sql_or_fail(sql);

    feature_flag_poller_poll_once_for_test();

    char out[64];
    feature_flag_poller_variation(g_test_flag_key, "fallback", out, sizeof(out));
    TEST_ASSERT_EQUAL_STRING("fallback", out);
}

/* flag_key自体が存在しない場合もdefault_valueが返る(未登録のFeature Flagを
 * 参照してもクラッシュせずフォールバックすることの確認) */
static void test_variation_returns_fallback_when_not_found(void) {
    feature_flag_poller_poll_once_for_test();

    char out[64];
    feature_flag_poller_variation(g_test_flag_key, "fallback", out, sizeof(out));
    TEST_ASSERT_EQUAL_STRING("fallback", out);
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
    RUN_TEST(test_variation_returns_default_variation_when_enabled);
    RUN_TEST(test_variation_returns_fallback_when_disabled);
    RUN_TEST(test_variation_returns_fallback_when_not_found);
    return UNITY_END();
}
