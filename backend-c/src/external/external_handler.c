#include "external/external_handler.h"

#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "auth/auth_module.h"
#include "auth/external_auth.h"
#include "common/error.h"
#include "common/log.h"
#include "domain/task.h"
#include "external/external_query.h"
#include "flags/feature_flag_poller.h"
#include "http/task_json.h"
#include "repository/task_repository.h"

/* handler.cのsend_json/send_error(REST v1)と同じ理由で、実際に書き込んだ
 * HTTPステータスをそのまま返す(呼び出し側のexternal_task_request_handlerが
 * リクエスト単位のログにこの値を使う) */
static int send_json(struct mg_connection *conn, int status, const char *body) {
    const char *status_text = (status == 200) ? "OK" : "OK";
    mg_printf(conn,
              "HTTP/1.1 %d %s\r\n"
              "Content-Type: application/json\r\n"
              "Content-Length: %zu\r\n"
              "Connection: close\r\n\r\n",
              status, status_text, strlen(body));
    mg_write(conn, body, strlen(body));
    return status;
}

static int send_error(struct mg_connection *conn, TaskError err) {
    char *body = task_error_json_body(err, NULL);
    int status = send_json(conn, task_error_http_status(err), body != NULL ? body : "{}");
    free(body);
    return status;
}

static int handle_list_offset(struct mg_connection *conn, int64_t user_id,
                               const char *query_string, size_t query_len) {
    int page = 1;
    int page_size = 10;
    external_parse_offset_paging(query_string, query_len, &page, &page_size);
    log_debugf("external debug: list_offset user_id=%lld page=%d page_size=%d\n",
               (long long)user_id, page, page_size);

    int offset = (page - 1) * page_size;
    Task **tasks = NULL;
    size_t count = 0;
    int64_t total = 0;
    TaskError err = task_repository_list(user_id, page_size, offset, &tasks, &count, &total);
    if (err != TASK_OK) {
        return send_error(conn, err);
    }

    cJSON *root = cJSON_CreateObject();
    cJSON *tasks_json = cJSON_AddArrayToObject(root, "tasks");
    for (size_t i = 0; i < count; i++) {
        cJSON_AddItemToArray(tasks_json, task_to_json(tasks[i]));
        task_destroy(tasks[i]);
    }
    free(tasks);
    cJSON_AddNumberToObject(root, "page", page);
    cJSON_AddNumberToObject(root, "page_size", page_size);
    cJSON_AddNumberToObject(root, "total", (double)total);

    char *body = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    int status = send_json(conn, 200, body != NULL ? body : "{}");
    free(body);
    return status;
}

static int handle_list_cursor(struct mg_connection *conn, int64_t user_id,
                               const char *query_string, size_t query_len) {
    int64_t after_id = 0;
    int limit = 10;
    external_parse_cursor_paging(query_string, query_len, &after_id, &limit);
    log_debugf("external debug: list_cursor user_id=%lld after_id=%lld limit=%d\n",
               (long long)user_id, (long long)after_id, limit);

    Task **tasks = NULL;
    size_t count = 0;
    int64_t next_cursor = 0;
    TaskError err =
        task_repository_list_cursor(user_id, after_id, limit, &tasks, &count, &next_cursor);
    if (err != TASK_OK) {
        return send_error(conn, err);
    }

    cJSON *root = cJSON_CreateObject();
    cJSON *tasks_json = cJSON_AddArrayToObject(root, "tasks");
    for (size_t i = 0; i < count; i++) {
        cJSON_AddItemToArray(tasks_json, task_to_json(tasks[i]));
        task_destroy(tasks[i]);
    }
    free(tasks);

    /* next_cursorはCONTRACT.mdセクション11の通り文字列(またはnull)で返す。
     * backend-cの内部規約(task_repository.h参照)ではカーソルは単なる数値idであり、
     * 0は「次ページ無し」を意味する */
    if (next_cursor > 0) {
        char cursor_str[32];
        snprintf(cursor_str, sizeof(cursor_str), "%lld", (long long)next_cursor);
        cJSON_AddStringToObject(root, "next_cursor", cursor_str);
    } else {
        cJSON_AddNullToObject(root, "next_cursor");
    }
    cJSON_AddNumberToObject(root, "limit", limit);

    char *body = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    int status = send_json(conn, 200, body != NULL ? body : "{}");
    free(body);
    return status;
}

/* REST v1(http/handler.cのlog_rest_request)と同じ形式・同じ計測方法
 * (README.md「各種ログの出力先」節参照) */
static void log_external_request(const char *method, const char *path, int status,
                                  const struct timespec *start) {
    struct timespec end;
    clock_gettime(CLOCK_MONOTONIC, &end);
    long duration_ms = (end.tv_sec - start->tv_sec) * 1000 +
                        (end.tv_nsec - start->tv_nsec) / 1000000;
    printf("external method=%s path=%s status=%d duration_ms=%ld\n", method, path, status,
           duration_ms);
}

int external_task_request_handler(struct mg_connection *conn, void *cbdata) {
    (void)cbdata;
    const struct mg_request_info *ri = mg_get_request_info(conn);
    struct timespec start;
    clock_gettime(CLOCK_MONOTONIC, &start);
    int status;

    if (strcmp(ri->request_method, "GET") != 0) {
        status = send_error(conn, TASK_ERR_INVALID_REQUEST);
        log_external_request(ri->request_method, ri->local_uri, status, &start);
        return 1;
    }

    const char *authorization = mg_get_header(conn, "Authorization");
    TaskError auth_err = auth_require_external_client(auth_module_dispatcher(), authorization,
                                                        auth_module_external_api_client_id());
    if (auth_err != TASK_OK) {
        status = send_error(conn, auth_err);
        log_external_request(ri->request_method, ri->local_uri, status, &start);
        return 1;
    }

    const char *query_string = ri->query_string;
    size_t query_len = query_string != NULL ? strlen(query_string) : 0;

    int64_t user_id = 0;
    TaskError user_id_err = external_parse_user_id(query_string, query_len, &user_id);
    if (user_id_err != TASK_OK) {
        status = send_error(conn, user_id_err);
        log_external_request(ri->request_method, ri->local_uri, status, &start);
        return 1;
    }

    /* backend.external-tasks-pagination-v2(全言語で共有する1つのFeature Flag)で
     * offset(v1)/cursor(v2)を切り替える(backend-cpp/backend-rustと同じ設計) */
    char variation[64];
    feature_flag_poller_variation("backend.external-tasks-pagination-v2", "off", variation,
                                   sizeof(variation));

    if (external_use_cursor_paging(variation)) {
        status = handle_list_cursor(conn, user_id, query_string, query_len);
    } else {
        status = handle_list_offset(conn, user_id, query_string, query_len);
    }
    log_external_request(ri->request_method, ri->local_uri, status, &start);
    return 1;
}
