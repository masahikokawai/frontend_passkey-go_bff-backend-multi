#ifndef COMMON_ERROR_H
#define COMMON_ERROR_H

/* backend-cpp/src/common/error.hppのAppErrorKindと同一の分類。
 * Cには例外が無いため、全ての関数がこのenumを戻り値として返す設計にする
 * (Repositoryより上位のレイヤーはこのenumだけを見ればよい) */
typedef enum {
    TASK_OK = 0,
    TASK_ERR_NOT_FOUND,
    TASK_ERR_INVALID_REQUEST,
    TASK_ERR_INVALID_ID,
    TASK_ERR_INVALID_STATUS,
    TASK_ERR_INVALID_FINISHED_ON,
    TASK_ERR_VALIDATION_ERROR,
    TASK_ERR_DB_ERROR,
    TASK_ERR_UNAUTHORIZED,
    TASK_ERR_MEMORY_ERROR,
    /* 外部公開API(src/external/external_handler.c)専用。CONTRACT.mdセクション11、
     * backend-cpp/backend-rustと同じ"error"文字列("invalid_request"等に丸めない) */
    TASK_ERR_USER_ID_REQUIRED,
    TASK_ERR_INVALID_USER_ID,
} TaskError;

int task_error_http_status(TaskError err);

/* task_error_json_body: エラーのJSONボディを返す(呼び出し側がfree()すること)
 * validation_messageはTASK_ERR_VALIDATION_ERRORのときのみ使用する(NULL可) */
char *task_error_json_body(TaskError err, const char *validation_message);

#endif /* COMMON_ERROR_H */
