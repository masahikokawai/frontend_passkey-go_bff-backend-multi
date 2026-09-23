/*
 * REST/外部公開API共通のエラーコード→HTTPステータス・JSONボディ変換
 * (src/common/error.c: task_error_http_status / task_error_json_body)の単体テスト。
 * backend-java/src/test/java/com/bffgin/backend/rest/RestErrorMapperTest.java
 * (gold standard、CONTRACT.mdセクション20.5のワイヤー契約パリティ)と同じ観点だが、
 * backend-cのTaskError enumはuser_not_provisionedを独立した種別として持たず
 * (auth/user_resolver.cのコメント通りUNAUTHORIZEDへ丸め込む設計)、代わりに
 * 外部公開API専用のTASK_ERR_USER_ID_REQUIRED/TASK_ERR_INVALID_USER_IDを持つため、
 * それらenum実体に合わせてケースを揃えている(実装をJavaに寄せて書き換えたりはしない)。
 * DB・実サーバー・ネットワーク一切不要な純粋関数のみを対象とする
 */
#include <stdlib.h>
#include <string.h>

#include <cjson/cJSON.h>

#include "unity.h"

#include "common/error.h"

void setUp(void) {}
void tearDown(void) {}

/* task_error_json_bodyの戻り値(呼び出し側がfree()する契約)をパースし、
 * "error"キーの文字列が期待通りであることを確認した上でfreeする */
static void assert_error_code(TaskError err, const char *validation_message,
                               const char *expected_code) {
    char *body = task_error_json_body(err, validation_message);
    TEST_ASSERT_NOT_NULL(body);
    cJSON *root = cJSON_Parse(body);
    TEST_ASSERT_NOT_NULL(root);
    cJSON *error_item = cJSON_GetObjectItemCaseSensitive(root, "error");
    TEST_ASSERT_NOT_NULL(error_item);
    TEST_ASSERT_TRUE(cJSON_IsString(error_item));
    TEST_ASSERT_EQUAL_STRING(expected_code, error_item->valuestring);
    cJSON_Delete(root);
    free(body);
}

/* 内部REST(src/http/handler.c)向けの成功ステータスのみを持つケース。
 * 200応答自体はこの変換関数を通らない実装だが、task_error_http_status(TASK_OK)は
 * それ単体で意味のある変換(REST/gRPC双方の成功マッピングの回帰確認)として残す */
static void test_ok_maps_to_200(void) {
    TEST_ASSERT_EQUAL_INT(200, task_error_http_status(TASK_OK));
}

static void test_not_found_maps_to_404(void) {
    TEST_ASSERT_EQUAL_INT(404, task_error_http_status(TASK_ERR_NOT_FOUND));
    assert_error_code(TASK_ERR_NOT_FOUND, NULL, "not_found");
}

static void test_invalid_request_maps_to_400(void) {
    TEST_ASSERT_EQUAL_INT(400, task_error_http_status(TASK_ERR_INVALID_REQUEST));
    assert_error_code(TASK_ERR_INVALID_REQUEST, NULL, "invalid_request");
}

static void test_invalid_id_maps_to_400(void) {
    TEST_ASSERT_EQUAL_INT(400, task_error_http_status(TASK_ERR_INVALID_ID));
    assert_error_code(TASK_ERR_INVALID_ID, NULL, "invalid_id");
}

static void test_invalid_status_maps_to_422(void) {
    TEST_ASSERT_EQUAL_INT(422, task_error_http_status(TASK_ERR_INVALID_STATUS));
    assert_error_code(TASK_ERR_INVALID_STATUS, NULL, "invalid_status");
}

static void test_invalid_finished_on_maps_to_422(void) {
    TEST_ASSERT_EQUAL_INT(422, task_error_http_status(TASK_ERR_INVALID_FINISHED_ON));
    assert_error_code(TASK_ERR_INVALID_FINISHED_ON, NULL, "invalid_finished_on");
}

/* validation_errorのみ、"error"に加えて"message"キーへ入力メッセージをそのまま
 * 反映することを確認する(CONTRACT.mdセクション5.1、他言語のvalidation_errorと同じ形状) */
static void test_validation_error_maps_to_422_with_message(void) {
    TEST_ASSERT_EQUAL_INT(422, task_error_http_status(TASK_ERR_VALIDATION_ERROR));

    char *body = task_error_json_body(TASK_ERR_VALIDATION_ERROR, "nameは必須です");
    TEST_ASSERT_NOT_NULL(body);
    cJSON *root = cJSON_Parse(body);
    TEST_ASSERT_NOT_NULL(root);
    TEST_ASSERT_EQUAL_STRING("validation_error",
                              cJSON_GetObjectItemCaseSensitive(root, "error")->valuestring);
    cJSON *message_item = cJSON_GetObjectItemCaseSensitive(root, "message");
    TEST_ASSERT_NOT_NULL(message_item);
    TEST_ASSERT_TRUE(cJSON_IsString(message_item));
    TEST_ASSERT_EQUAL_STRING("nameは必須です", message_item->valuestring);
    cJSON_Delete(root);
    free(body);
}

/* validation_messageにNULLを渡した場合は"message"キー自体を省く(呼び出し元が
 * メッセージを持たない場合でもボディが壊れないことの確認) */
static void test_validation_error_with_null_message_omits_message_key(void) {
    char *body = task_error_json_body(TASK_ERR_VALIDATION_ERROR, NULL);
    TEST_ASSERT_NOT_NULL(body);
    cJSON *root = cJSON_Parse(body);
    TEST_ASSERT_NOT_NULL(root);
    TEST_ASSERT_EQUAL_STRING("validation_error",
                              cJSON_GetObjectItemCaseSensitive(root, "error")->valuestring);
    TEST_ASSERT_NULL(cJSON_GetObjectItemCaseSensitive(root, "message"));
    cJSON_Delete(root);
    free(body);
}

/* validation_error以外のエラー種別にvalidation_messageを渡しても無視され、"message"
 * キーが漏れ出さないことの確認(呼び出し側の実装ミスでメッセージを渡してしまっても
 * ワイヤー契約上の形状が崩れないための回帰テスト) */
static void test_non_validation_error_ignores_message_argument(void) {
    char *body = task_error_json_body(TASK_ERR_NOT_FOUND, "this message must not leak");
    TEST_ASSERT_NOT_NULL(body);
    cJSON *root = cJSON_Parse(body);
    TEST_ASSERT_NOT_NULL(root);
    TEST_ASSERT_EQUAL_STRING("not_found",
                              cJSON_GetObjectItemCaseSensitive(root, "error")->valuestring);
    TEST_ASSERT_NULL(cJSON_GetObjectItemCaseSensitive(root, "message"));
    cJSON_Delete(root);
    free(body);
}

/* CONTRACT.mdセクション11: 認証失敗は401 {"error":"unauthenticated"}
 * (external_auth.hのコメント・auth/user_resolver.cの「user not provisioned相当」も
 * ここに丸め込まれる、backend-cはJavaのような403 user_not_provisioned種別を持たない) */
static void test_unauthorized_maps_to_401(void) {
    TEST_ASSERT_EQUAL_INT(401, task_error_http_status(TASK_ERR_UNAUTHORIZED));
    assert_error_code(TASK_ERR_UNAUTHORIZED, NULL, "unauthenticated");
}

static void test_db_error_maps_to_500(void) {
    TEST_ASSERT_EQUAL_INT(500, task_error_http_status(TASK_ERR_DB_ERROR));
    assert_error_code(TASK_ERR_DB_ERROR, NULL, "internal_server_error");
}

static void test_memory_error_maps_to_500(void) {
    TEST_ASSERT_EQUAL_INT(500, task_error_http_status(TASK_ERR_MEMORY_ERROR));
    assert_error_code(TASK_ERR_MEMORY_ERROR, NULL, "internal_server_error");
}

/* 外部公開API(src/external/external_handler.c)専用のエラー種別。他の"invalid_*"に
 * 丸めず独立した"error"文字列を返す(CONTRACT.mdセクション11、external_query_test.cの
 * external_parse_user_idケースと対になる、こちらはHTTPステータス/ボディ形状側の確認) */
static void test_user_id_required_maps_to_400(void) {
    TEST_ASSERT_EQUAL_INT(400, task_error_http_status(TASK_ERR_USER_ID_REQUIRED));
    assert_error_code(TASK_ERR_USER_ID_REQUIRED, NULL, "user_id_required");
}

static void test_invalid_user_id_maps_to_400(void) {
    TEST_ASSERT_EQUAL_INT(400, task_error_http_status(TASK_ERR_INVALID_USER_ID));
    assert_error_code(TASK_ERR_INVALID_USER_ID, NULL, "invalid_user_id");
}

int main(void) {
    UNITY_BEGIN();
    RUN_TEST(test_ok_maps_to_200);
    RUN_TEST(test_not_found_maps_to_404);
    RUN_TEST(test_invalid_request_maps_to_400);
    RUN_TEST(test_invalid_id_maps_to_400);
    RUN_TEST(test_invalid_status_maps_to_422);
    RUN_TEST(test_invalid_finished_on_maps_to_422);
    RUN_TEST(test_validation_error_maps_to_422_with_message);
    RUN_TEST(test_validation_error_with_null_message_omits_message_key);
    RUN_TEST(test_non_validation_error_ignores_message_argument);
    RUN_TEST(test_unauthorized_maps_to_401);
    RUN_TEST(test_db_error_maps_to_500);
    RUN_TEST(test_memory_error_maps_to_500);
    RUN_TEST(test_user_id_required_maps_to_400);
    RUN_TEST(test_invalid_user_id_maps_to_400);
    return UNITY_END();
}
