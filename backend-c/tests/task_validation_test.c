#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "unity.h"

#include "application/task_validation.h"
#include "common/time.h"
#include "domain/task.h"

void setUp(void) {}
void tearDown(void) {}

/* backend-cpp/tests/model_test.cppのStatus.RoundTripと同じ */
static void test_status_round_trip(void) {
    TEST_ASSERT_EQUAL_STRING("waiting", task_status_to_string(TASK_STATUS_WAITING));
    TEST_ASSERT_EQUAL_STRING("work_in_progress", task_status_to_string(TASK_STATUS_WORK_IN_PROGRESS));
    TEST_ASSERT_EQUAL_STRING("completed", task_status_to_string(TASK_STATUS_COMPLETED));

    TaskStatus status;
    TEST_ASSERT_EQUAL_INT(0, task_status_from_string("waiting", &status));
    TEST_ASSERT_EQUAL_INT(TASK_STATUS_WAITING, status);
    TEST_ASSERT_EQUAL_INT(-1, task_status_from_string("bogus", &status));
}

/* backend-cpp/tests/model_test.cppのIsValidIsoDate.RejectsCalendarInvalidDateと同じ
 * (2026-02-30のようなカレンダー上存在しない日付を弾けるかの回帰テスト) */
static void test_is_valid_iso_date(void) {
    TEST_ASSERT_TRUE(task_is_valid_iso_date("2026-02-28"));
    TEST_ASSERT_FALSE(task_is_valid_iso_date("2026-02-30"));
    TEST_ASSERT_TRUE(task_is_valid_iso_date("2024-02-29"));
    TEST_ASSERT_FALSE(task_is_valid_iso_date("2026-13-01"));
    TEST_ASSERT_FALSE(task_is_valid_iso_date("not-a-date"));
}

/* backend-cpp/tests/model_test.cppのUtf8CodepointLength.CountsCodepointsNotBytesと同じ
 * (過去にbackend-scala-http4sで見つかったUTF-16コード単位数バグの回帰防止) */
static void test_utf8_codepoint_length(void) {
    TEST_ASSERT_EQUAL_UINT(5, task_utf8_codepoint_length("hello"));
    TEST_ASSERT_EQUAL_UINT(3, task_utf8_codepoint_length("\xE3\x81\x82\xE3\x81\x84\xE3\x81\x86"));
    TEST_ASSERT_EQUAL_UINT(1, task_utf8_codepoint_length("\xF0\x9F\x98\x80"));
}

static void test_today_utc_iso_returns_valid_date(void) {
    char today[11];
    task_today_utc_iso(today, sizeof(today));
    TEST_ASSERT_EQUAL_UINT(10, strlen(today));
    TEST_ASSERT_TRUE(task_is_valid_iso_date(today));
}

/* nameが21コードポイント(20文字制限を1文字超過)ならvalidation_errorになることの確認 */
static void test_validate_input_rejects_name_over_20_codepoints(void) {
    TaskInput *input = task_input_create();
    task_input_set_name(input, "aaaaaaaaaaaaaaaaaaaaa"); /* 21文字 */
    char future[11];
    task_today_utc_iso(future, sizeof(future));
    snprintf(input->finished_on, sizeof(input->finished_on), "%s", "2999-01-01");
    snprintf(input->status_raw, sizeof(input->status_raw), "%s", "waiting");

    TaskStatus status;
    char *message = NULL;
    TaskError err = task_validate_input(input, &status, &message);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_VALIDATION_ERROR, err);
    TEST_ASSERT_NOT_NULL(message);
    free(message);
    task_input_destroy(input);
}

/* finished_onが過去日ならvalidation_errorになることの確認(UTC基準) */
static void test_validate_input_rejects_past_finished_on(void) {
    TaskInput *input = task_input_create();
    task_input_set_name(input, "ok");
    snprintf(input->finished_on, sizeof(input->finished_on), "%s", "2000-01-01");
    snprintf(input->status_raw, sizeof(input->status_raw), "%s", "waiting");

    TaskStatus status;
    char *message = NULL;
    TaskError err = task_validate_input(input, &status, &message);
    TEST_ASSERT_EQUAL_INT(TASK_ERR_VALIDATION_ERROR, err);
    TEST_ASSERT_NOT_NULL(message);
    free(message);
    task_input_destroy(input);
}

static void test_validate_input_accepts_valid_input(void) {
    TaskInput *input = task_input_create();
    task_input_set_name(input, "valid task");
    snprintf(input->finished_on, sizeof(input->finished_on), "%s", "2999-01-01");
    snprintf(input->status_raw, sizeof(input->status_raw), "%s", "completed");

    TaskStatus status;
    char *message = NULL;
    TaskError err = task_validate_input(input, &status, &message);
    TEST_ASSERT_EQUAL_INT(TASK_OK, err);
    TEST_ASSERT_NULL(message);
    TEST_ASSERT_EQUAL_INT(TASK_STATUS_COMPLETED, status);
    task_input_destroy(input);
}

int main(void) {
    UNITY_BEGIN();
    RUN_TEST(test_status_round_trip);
    RUN_TEST(test_is_valid_iso_date);
    RUN_TEST(test_utf8_codepoint_length);
    RUN_TEST(test_today_utc_iso_returns_valid_date);
    RUN_TEST(test_validate_input_rejects_name_over_20_codepoints);
    RUN_TEST(test_validate_input_rejects_past_finished_on);
    RUN_TEST(test_validate_input_accepts_valid_input);
    return UNITY_END();
}
