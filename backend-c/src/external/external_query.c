#include "external/external_query.h"

#include <civetweb.h>
#include <stdlib.h>
#include <string.h>

TaskError external_parse_user_id(const char *query_string, size_t query_len,
                                  int64_t *out_user_id) {
    if (query_string == NULL) return TASK_ERR_USER_ID_REQUIRED;

    char buf[32];
    int len = mg_get_var(query_string, query_len, "user_id", buf, sizeof(buf));
    if (len <= 0) return TASK_ERR_USER_ID_REQUIRED;

    char *endptr = NULL;
    long long v = strtoll(buf, &endptr, 10);
    if (endptr == buf || *endptr != '\0') return TASK_ERR_INVALID_USER_ID;
    *out_user_id = (int64_t)v;
    return TASK_OK;
}

void external_parse_offset_paging(const char *query_string, size_t query_len, int *out_page,
                                   int *out_page_size) {
    int page = 1;
    int page_size = 10;
    if (query_string != NULL) {
        char buf[32];
        if (mg_get_var(query_string, query_len, "page", buf, sizeof(buf)) > 0) {
            page = atoi(buf);
        }
        if (mg_get_var(query_string, query_len, "page_size", buf, sizeof(buf)) > 0) {
            page_size = atoi(buf);
        }
    }
    *out_page = page < 1 ? 1 : page;
    *out_page_size = page_size < 1 ? 1 : page_size;
}

void external_parse_cursor_paging(const char *query_string, size_t query_len,
                                   int64_t *out_after_id, int *out_limit) {
    int64_t after_id = 0;
    int limit = 10;
    if (query_string != NULL) {
        char buf[32];
        if (mg_get_var(query_string, query_len, "cursor", buf, sizeof(buf)) > 0) {
            /* 不正な値(数値でない)は「カーソル省略=先頭から」と同じ扱いにする
             * (backend-cppのlist_v2と同じ、寛容な扱い。cursorは不透明なトークンという
             * 位置づけのため、壊れた値を400ではなく先頭ページへのフォールバックにする) */
            char *endptr = NULL;
            long long v = strtoll(buf, &endptr, 10);
            if (endptr != buf && *endptr == '\0') after_id = (int64_t)v;
        }
        if (mg_get_var(query_string, query_len, "limit", buf, sizeof(buf)) > 0) {
            limit = atoi(buf);
        }
    }
    *out_after_id = after_id;
    *out_limit = limit < 1 ? 1 : limit;
}

int external_use_cursor_paging(const char *variation) {
    return variation != NULL && strcmp(variation, "on") == 0;
}
