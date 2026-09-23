#ifndef AUTH_USER_RESOLVER_H
#define AUTH_USER_RESOLVER_H

#include <stdint.h>

#include "auth/dispatcher.h"
#include "common/error.h"

/*
 * Authorizationヘッダの値("Bearer xxx"、無ければNULL)からuser_idを解決する。
 * REST(src/http/handler.c)・gRPC(src/grpc/grpc_server.c)の両トランスポートから
 * 共通で呼ばれる(backend-cppのapplication::ResolveUserIdFromAuthHeaderと同じ、
 * 認証ロジックをトランスポートごとに複製しないための共通化)
 */
TaskError auth_resolve_user_id(Dispatcher *dispatcher, const char *authorization_header,
                                int64_t *out_user_id);

#endif /* AUTH_USER_RESOLVER_H */
