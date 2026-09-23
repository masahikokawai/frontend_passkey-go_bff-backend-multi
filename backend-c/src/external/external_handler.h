#ifndef EXTERNAL_EXTERNAL_HANDLER_H
#define EXTERNAL_EXTERNAL_HANDLER_H

#include <civetweb.h>

/*
 * CONTRACT.mdセクション11「外部公開API」。内部REST v1(src/http/handler.c)・
 * 内部gRPC v2(src/grpc/grpc_server.c)とは別に、専用のCivetWebコンテキスト
 * (main.cで別ポートにmg_startする、"/external/v1/tasks"のみ登録)から呼ばれる
 */
int external_task_request_handler(struct mg_connection *conn, void *cbdata);

#endif /* EXTERNAL_EXTERNAL_HANDLER_H */
