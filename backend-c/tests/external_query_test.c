/*
 * 外部公開API(src/external/external_query.c)のクエリパース処理の単体テスト。
 * 実サーバー・DB不要(文字列を渡すだけの純粋関数)
 */
#include <stdint.h>
#include <string.h>

#include "unity.h"

#include "external/external_query.h"

void setUp(void) {}
void tearDown(void) {}

static void test_parse_user_id_required_when_missing(void) {
    int64_t user_id = 0;
    const char *q = "page=1";
    TEST_ASSERT_EQUAL_INT(TASK_ERR_USER_ID_REQUIRED,
                           external_parse_user_id(q, strlen(q), &user_id));
}

static void test_parse_user_id_required_when_empty(void) {
    int64_t user_id = 0;
    const char *q = "user_id=&page=1";
    TEST_ASSERT_EQUAL_INT(TASK_ERR_USER_ID_REQUIRED,
                           external_parse_user_id(q, strlen(q), &user_id));
}

static void test_parse_user_id_invalid_when_not_numeric(void) {
    int64_t user_id = 0;
    const char *q = "user_id=abc";
    TEST_ASSERT_EQUAL_INT(TASK_ERR_INVALID_USER_ID,
                           external_parse_user_id(q, strlen(q), &user_id));
}

static void test_parse_user_id_ok(void) {
    int64_t user_id = 0;
    const char *q = "user_id=42&page=2";
    TEST_ASSERT_EQUAL_INT(TASK_OK, external_parse_user_id(q, strlen(q), &user_id));
    TEST_ASSERT_EQUAL_INT64(42, user_id);
}

static void test_offset_paging_defaults(void) {
    int page = 0;
    int page_size = 0;
    external_parse_offset_paging(NULL, 0, &page, &page_size);
    TEST_ASSERT_EQUAL_INT(1, page);
    TEST_ASSERT_EQUAL_INT(10, page_size);
}

static void test_offset_paging_clamps_below_one(void) {
    int page = 0;
    int page_size = 0;
    const char *q = "page=0&page_size=-5";
    external_parse_offset_paging(q, strlen(q), &page, &page_size);
    TEST_ASSERT_EQUAL_INT(1, page);
    TEST_ASSERT_EQUAL_INT(1, page_size);
}

static void test_offset_paging_reads_explicit_values(void) {
    int page = 0;
    int page_size = 0;
    const char *q = "page=3&page_size=25";
    external_parse_offset_paging(q, strlen(q), &page, &page_size);
    TEST_ASSERT_EQUAL_INT(3, page);
    TEST_ASSERT_EQUAL_INT(25, page_size);
}

static void test_cursor_paging_defaults_to_start(void) {
    int64_t after_id = -1;
    int limit = 0;
    external_parse_cursor_paging(NULL, 0, &after_id, &limit);
    TEST_ASSERT_EQUAL_INT64(0, after_id);
    TEST_ASSERT_EQUAL_INT(10, limit);
}

static void test_cursor_paging_reads_cursor_and_limit(void) {
    int64_t after_id = 0;
    int limit = 0;
    const char *q = "cursor=123&limit=5";
    external_parse_cursor_paging(q, strlen(q), &after_id, &limit);
    TEST_ASSERT_EQUAL_INT64(123, after_id);
    TEST_ASSERT_EQUAL_INT(5, limit);
}

static void test_cursor_paging_ignores_garbage_cursor(void) {
    int64_t after_id = -1;
    int limit = 0;
    const char *q = "cursor=not-a-number&limit=5";
    external_parse_cursor_paging(q, strlen(q), &after_id, &limit);
    TEST_ASSERT_EQUAL_INT64(0, after_id);
    TEST_ASSERT_EQUAL_INT(5, limit);
}

static void test_cursor_paging_clamps_limit_below_one(void) {
    int64_t after_id = 0;
    int limit = 0;
    const char *q = "limit=0";
    external_parse_cursor_paging(q, strlen(q), &after_id, &limit);
    TEST_ASSERT_EQUAL_INT(1, limit);
}

static void test_use_cursor_paging(void) {
    TEST_ASSERT_TRUE(external_use_cursor_paging("on"));
    TEST_ASSERT_FALSE(external_use_cursor_paging("off"));
    TEST_ASSERT_FALSE(external_use_cursor_paging(""));
    TEST_ASSERT_FALSE(external_use_cursor_paging(NULL));
}

int main(void) {
    UNITY_BEGIN();
    RUN_TEST(test_parse_user_id_required_when_missing);
    RUN_TEST(test_parse_user_id_required_when_empty);
    RUN_TEST(test_parse_user_id_invalid_when_not_numeric);
    RUN_TEST(test_parse_user_id_ok);
    RUN_TEST(test_offset_paging_defaults);
    RUN_TEST(test_offset_paging_clamps_below_one);
    RUN_TEST(test_offset_paging_reads_explicit_values);
    RUN_TEST(test_cursor_paging_defaults_to_start);
    RUN_TEST(test_cursor_paging_reads_cursor_and_limit);
    RUN_TEST(test_cursor_paging_ignores_garbage_cursor);
    RUN_TEST(test_cursor_paging_clamps_limit_below_one);
    RUN_TEST(test_use_cursor_paging);
    return UNITY_END();
}
