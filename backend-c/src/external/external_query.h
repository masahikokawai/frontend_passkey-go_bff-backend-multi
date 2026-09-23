#ifndef EXTERNAL_EXTERNAL_QUERY_H
#define EXTERNAL_EXTERNAL_QUERY_H

#include <stddef.h>
#include <stdint.h>

#include "common/error.h"

/*
 * external_handler.cのクエリパース処理を、CivetWebのmg_connectionから独立した
 * 純粋関数として切り出したもの(tests/external_query_test.cから実サーバー無しに
 * 直接テストできるようにするため)
 */

/* user_idクエリパラメータを解析する。無い/空ならTASK_ERR_USER_ID_REQUIRED、
 * 数値として解釈できなければTASK_ERR_INVALID_USER_ID */
TaskError external_parse_user_id(const char *query_string, size_t query_len,
                                  int64_t *out_user_id);

/* v1(offsetページング)のpage/page_sizeを解析する。省略時はpage=1/page_size=10、
 * 1未満の値は1に切り上げる(CONTRACT.mdセクション11) */
void external_parse_offset_paging(const char *query_string, size_t query_len, int *out_page,
                                   int *out_page_size);

/* v2(cursorページング)のcursor/limitを解析する。cursor省略時・数値でない場合は
 * out_after_id=0(task_repository_list_cursorの規約通り「先頭から」を意味する)、
 * limitは省略時10・1未満は1に切り上げる */
void external_parse_cursor_paging(const char *query_string, size_t query_len,
                                   int64_t *out_after_id, int *out_limit);

/* Feature Flag `backend.external-tasks-pagination-v2` の評価結果("on"文字列かどうか)から
 * cursorページング(v2)を使うかどうかを判定する */
int external_use_cursor_paging(const char *variation);

#endif /* EXTERNAL_EXTERNAL_QUERY_H */
