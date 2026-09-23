#ifndef HTTP_HANDLER_H
#define HTTP_HANDLER_H

#include <civetweb.h>

/*
 * 【CivetWebの既知の挙動への対応】get_request_handler()は「完全一致」→「uri配下すべてに
 * マッチする部分一致(ワイルドカード不要)」→「パターンマッチ(*等)」の3段階で処理される
 * ため、"/internal/v1/tasks"とワイルドカード付きの子パスパターンを別々に登録すると、
 * 個別リソース向けリクエスト("/internal/v1/tasks/42")は常に2段階目で
 * "/internal/v1/tasks"(コレクション)ハンドラの部分一致に吸収されてしまい、
 * ワイルドカードハンドラには絶対に到達しない。そのため"/internal/v1/tasks"1つだけに
 * ハンドラを登録し、この関数内でURIの残り部分を見てコレクション/個別リソースを
 * 振り分ける(main.c参照)
 */
int task_request_handler(struct mg_connection *conn, void *cbdata);

#endif /* HTTP_HANDLER_H */
