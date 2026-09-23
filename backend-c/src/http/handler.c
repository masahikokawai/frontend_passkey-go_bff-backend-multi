#include "http/handler.h"

#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "application/task_validation.h"
#include "auth/auth_module.h"
#include "auth/user_resolver.h"
#include "common/error.h"
#include "common/log.h"
#include "common/time.h"
#include "domain/task.h"
#include "http/task_json.h"
#include "repository/task_repository.h"

#define BODY_BUF_LEN 65536

/* JWT/JWKS本実装(auth/dispatcher.h・auth/user_resolver.h)へ委譲する。REST/gRPC共通の
 * auth_resolve_user_idを使うことで認証ロジックをトランスポートごとに複製しない
 * (backend-cpp/backend-rustと同じ設計、README.md「認証について」参照) */
static TaskError resolve_user_id(struct mg_connection *conn, int64_t *out_user_id) {
    const char *authorization = mg_get_header(conn, "Authorization");
    return auth_resolve_user_id(auth_module_dispatcher(), authorization, out_user_id);
}

/* いずれもmg_write済みの実際のHTTPステータスをそのまま返す(呼び出し側の
 * task_request_handlerがリクエスト単位のログにこの値をそのまま使う。ログのために
 * ステータスを推測・再計算する経路を別に持たない、README.md「各種ログ」節参照) */
static int send_json(struct mg_connection *conn, int status, const char *body) {
    const char *status_text = (status == 200)   ? "OK"
                               : (status == 201) ? "Created"
                                                  : "OK";
    mg_printf(conn,
              "HTTP/1.1 %d %s\r\n"
              "Content-Type: application/json\r\n"
              "Content-Length: %zu\r\n"
              "Connection: close\r\n\r\n",
              status, status_text, strlen(body));
    mg_write(conn, body, strlen(body));
    return status;
}

static int send_no_content(struct mg_connection *conn) {
    mg_printf(conn,
              "HTTP/1.1 204 No Content\r\n"
              "Connection: close\r\n\r\n");
    return 204;
}

static int send_error(struct mg_connection *conn, TaskError err, const char *message) {
    char *body = task_error_json_body(err, message);
    int status = send_json(conn, task_error_http_status(err), body != NULL ? body : "{}");
    free(body);
    return status;
}

/*
 * 【メモリ管理のバッド/グッドプラクティス】クライアントが送るボディ長は信用できない
 * (Content-Lengthヘッダを詐称される、あるいはヘッダ自体を検証せず使う可能性がある)。
 * 以下のように「読み込み先の残り容量」を渡さずmg_readを呼ぶと、想定より大きいボディを
 * 送られた際にヒープバッファをオーバーフローする(CWE-787、境界外書き込み):
 *
 *   int n = mg_read(conn, buf + total, BODY_BUF_LEN);  // ← 常に固定サイズを渡してしまう
 *   total += (size_t)n;                                 // ← totalがBODY_BUF_LENを超えても止まらない
 *
 * 修正後: 毎回「残り容量(BODY_BUF_LEN - 1 - total)」を計算して渡し、
 * 残り容量が尽きたら明示的にループを打ち切る(NUL終端の1バイト分を常に確保する) */
static char *read_body(struct mg_connection *conn) {
    char *buf = (char *)malloc(BODY_BUF_LEN);
    if (buf == NULL) return NULL;
    size_t total = 0;
    for (;;) {
        int n = mg_read(conn, buf + total, BODY_BUF_LEN - 1 - total);
        if (n <= 0) break;
        total += (size_t)n;
        if (total >= BODY_BUF_LEN - 1) break;
    }
    buf[total] = '\0';
    return buf;
}

/* URIから末尾のid部分を取り出す(例: "/internal/v1/tasks/42" -> 42)
 * 戻り値: 成功時0、id部分が空/数値以外ならTASK_ERR_INVALID_IDを意味する-1 */
static int parse_id_from_uri(const char *uri, int64_t *out_id) {
    const char *prefix = "/internal/v1/tasks/";
    size_t prefix_len = strlen(prefix);
    if (strncmp(uri, prefix, prefix_len) != 0) return -1;
    const char *id_part = uri + prefix_len;
    if (id_part[0] == '\0') return -1;
    char *endptr = NULL;
    long long v = strtoll(id_part, &endptr, 10);
    if (endptr == id_part || *endptr != '\0') return -1;
    *out_id = (int64_t)v;
    return 0;
}

static int is_collection_uri(const char *uri) {
    return strcmp(uri, "/internal/v1/tasks") == 0;
}

static int handle_list(struct mg_connection *conn, int64_t user_id) {
    const struct mg_request_info *ri = mg_get_request_info(conn);
    int limit = 20;
    int offset = 0;
    if (ri->query_string != NULL) {
        char val[32];
        if (mg_get_var(ri->query_string, strlen(ri->query_string), "limit", val, sizeof(val)) > 0) {
            limit = atoi(val);
        }
        if (mg_get_var(ri->query_string, strlen(ri->query_string), "offset", val, sizeof(val)) >
            0) {
            offset = atoi(val);
        }
    }
    log_debugf("rest debug: list user_id=%lld limit=%d offset=%d\n", (long long)user_id, limit,
               offset);

    Task **tasks = NULL;
    size_t count = 0;
    int64_t total = 0;
    TaskError err = task_repository_list(user_id, limit, offset, &tasks, &count, &total);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }

    cJSON *root = cJSON_CreateObject();
    cJSON *tasks_json = cJSON_AddArrayToObject(root, "tasks");
    for (size_t i = 0; i < count; i++) {
        cJSON_AddItemToArray(tasks_json, task_to_json(tasks[i]));
        task_destroy(tasks[i]);
    }
    free(tasks);
    cJSON_AddNumberToObject(root, "total", (double)total);
    cJSON_AddNumberToObject(root, "limit", limit);
    cJSON_AddNumberToObject(root, "offset", offset);

    char *body = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    int status = send_json(conn, 200, body != NULL ? body : "{}");
    free(body);
    return status;
}

static int handle_get(struct mg_connection *conn, int64_t id, int64_t user_id) {
    Task *task = NULL;
    TaskError err = task_repository_find_by_id(id, user_id, &task);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }
    cJSON *root = task_to_json(task);
    task_destroy(task);
    char *body = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    int status = send_json(conn, 200, body != NULL ? body : "{}");
    free(body);
    return status;
}

static int handle_create(struct mg_connection *conn, int64_t user_id) {
    char *raw_body = read_body(conn);
    if (raw_body == NULL) {
        return send_error(conn, TASK_ERR_MEMORY_ERROR, NULL);
    }
    cJSON *json = cJSON_Parse(raw_body);
    free(raw_body);
    if (json == NULL) {
        return send_error(conn, TASK_ERR_INVALID_REQUEST, NULL);
    }

    TaskInput *input = NULL;
    TaskError err = task_parse_input(json, &input);
    cJSON_Delete(json);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }

    TaskStatus status;
    char *message = NULL;
    err = task_validate_input(input, &status, &message);
    if (err != TASK_OK) {
        task_input_destroy(input);
        int http_status = send_error(conn, err, message);
        free(message);
        return http_status;
    }

    int64_t new_id = 0;
    err = task_repository_create(user_id, input, &new_id);
    task_input_destroy(input);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }
    log_debugf("rest debug: create user_id=%lld new_id=%lld\n", (long long)user_id,
               (long long)new_id);

    Task *task = NULL;
    err = task_repository_find_by_id(new_id, user_id, &task);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }
    cJSON *root = task_to_json(task);
    task_destroy(task);
    char *body = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    int http_status = send_json(conn, 201, body != NULL ? body : "{}");
    free(body);
    return http_status;
}

static int handle_update(struct mg_connection *conn, int64_t id, int64_t user_id) {
    char *raw_body = read_body(conn);
    if (raw_body == NULL) {
        return send_error(conn, TASK_ERR_MEMORY_ERROR, NULL);
    }
    cJSON *json = cJSON_Parse(raw_body);
    free(raw_body);
    if (json == NULL) {
        return send_error(conn, TASK_ERR_INVALID_REQUEST, NULL);
    }

    TaskInput *input = NULL;
    TaskError err = task_parse_input(json, &input);
    cJSON_Delete(json);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }

    TaskStatus status;
    char *message = NULL;
    err = task_validate_input(input, &status, &message);
    if (err != TASK_OK) {
        task_input_destroy(input);
        int http_status = send_error(conn, err, message);
        free(message);
        return http_status;
    }

    err = task_repository_update(id, user_id, input);
    task_input_destroy(input);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }

    Task *task = NULL;
    err = task_repository_find_by_id(id, user_id, &task);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }
    cJSON *root = task_to_json(task);
    task_destroy(task);
    char *body = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    int http_status = send_json(conn, 200, body != NULL ? body : "{}");
    free(body);
    return http_status;
}

static int handle_delete(struct mg_connection *conn, int64_t id, int64_t user_id) {
    TaskError err = task_repository_delete(id, user_id);
    if (err != TASK_OK) {
        return send_error(conn, err, NULL);
    }
    return send_no_content(conn);
}

/* リクエスト単位のログ(README.md「各種ログの出力先」節参照)。gRPC側
 * (grpc/grpc_server.cのprintf("grpc method=..."))と同じkey=value形式・同じ
 * CLOCK_MONOTONICによる計測方法にそろえている。ログに使うステータスは
 * send_json/send_no_content/send_errorが実際に書き込んだ値そのもの(推測しない) */
static void log_rest_request(const char *method, const char *path, int status,
                              const struct timespec *start) {
    struct timespec end;
    clock_gettime(CLOCK_MONOTONIC, &end);
    long duration_ms = (end.tv_sec - start->tv_sec) * 1000 +
                        (end.tv_nsec - start->tv_nsec) / 1000000;
    printf("rest method=%s path=%s status=%d duration_ms=%ld\n", method, path, status,
           duration_ms);
}

int task_request_handler(struct mg_connection *conn, void *cbdata) {
    (void)cbdata;
    const struct mg_request_info *ri = mg_get_request_info(conn);
    struct timespec start;
    clock_gettime(CLOCK_MONOTONIC, &start);
    int status;

    int64_t user_id = 0;
    TaskError auth_err = resolve_user_id(conn, &user_id);
    if (auth_err != TASK_OK) {
        status = send_error(conn, auth_err, NULL);
        log_rest_request(ri->request_method, ri->local_uri, status, &start);
        return 1;
    }
    log_debugf("rest debug: authenticated user_id=%lld method=%s path=%s\n", (long long)user_id,
               ri->request_method, ri->local_uri);

    if (is_collection_uri(ri->local_uri)) {
        if (strcmp(ri->request_method, "GET") == 0) {
            status = handle_list(conn, user_id);
        } else if (strcmp(ri->request_method, "POST") == 0) {
            status = handle_create(conn, user_id);
        } else {
            status = send_error(conn, TASK_ERR_INVALID_REQUEST, NULL);
        }
        log_rest_request(ri->request_method, ri->local_uri, status, &start);
        return 1;
    }

    int64_t id = 0;
    if (parse_id_from_uri(ri->local_uri, &id) != 0) {
        status = send_error(conn, TASK_ERR_INVALID_ID, NULL);
        log_rest_request(ri->request_method, ri->local_uri, status, &start);
        return 1;
    }

    if (strcmp(ri->request_method, "GET") == 0) {
        status = handle_get(conn, id, user_id);
    } else if (strcmp(ri->request_method, "PATCH") == 0) {
        status = handle_update(conn, id, user_id);
    } else if (strcmp(ri->request_method, "DELETE") == 0) {
        status = handle_delete(conn, id, user_id);
    } else {
        status = send_error(conn, TASK_ERR_INVALID_REQUEST, NULL);
    }
    log_rest_request(ri->request_method, ri->local_uri, status, &start);
    return 1;
}
