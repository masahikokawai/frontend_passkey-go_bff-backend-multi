#include "common/error.h"

#include <cjson/cJSON.h>
#include <stdlib.h>

int task_error_http_status(TaskError err) {
    switch (err) {
        case TASK_OK:
            return 200;
        case TASK_ERR_NOT_FOUND:
            return 404;
        case TASK_ERR_INVALID_REQUEST:
        case TASK_ERR_INVALID_ID:
        case TASK_ERR_USER_ID_REQUIRED:
        case TASK_ERR_INVALID_USER_ID:
            return 400;
        case TASK_ERR_INVALID_STATUS:
        case TASK_ERR_INVALID_FINISHED_ON:
        case TASK_ERR_VALIDATION_ERROR:
            return 422;
        case TASK_ERR_UNAUTHORIZED:
            return 401;
        case TASK_ERR_DB_ERROR:
        case TASK_ERR_MEMORY_ERROR:
        default:
            return 500;
    }
}

/* JSON形状はbackend-cpp/backend-js-expressのerror.jsと1文字も変えていない
 * (CONTRACT.mdセクション5.1、"error"キー+ validation_errorのみ"message"併記) */
char *task_error_json_body(TaskError err, const char *validation_message) {
    cJSON *root = cJSON_CreateObject();
    const char *code;
    switch (err) {
        case TASK_ERR_NOT_FOUND:
            code = "not_found";
            break;
        case TASK_ERR_INVALID_REQUEST:
            code = "invalid_request";
            break;
        case TASK_ERR_INVALID_ID:
            code = "invalid_id";
            break;
        case TASK_ERR_INVALID_STATUS:
            code = "invalid_status";
            break;
        case TASK_ERR_INVALID_FINISHED_ON:
            code = "invalid_finished_on";
            break;
        case TASK_ERR_VALIDATION_ERROR:
            code = "validation_error";
            break;
        case TASK_ERR_UNAUTHORIZED:
            code = "unauthenticated";
            break;
        case TASK_ERR_USER_ID_REQUIRED:
            code = "user_id_required";
            break;
        case TASK_ERR_INVALID_USER_ID:
            code = "invalid_user_id";
            break;
        case TASK_ERR_DB_ERROR:
        case TASK_ERR_MEMORY_ERROR:
        default:
            code = "internal_server_error";
            break;
    }
    cJSON_AddStringToObject(root, "error", code);
    if (err == TASK_ERR_VALIDATION_ERROR && validation_message != NULL) {
        cJSON_AddStringToObject(root, "message", validation_message);
    }
    char *out = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    return out; /* 呼び出し側がfree()すること(cJSON_PrintUnformattedはmallocで確保する) */
}
