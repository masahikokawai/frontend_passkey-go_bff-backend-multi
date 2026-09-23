#ifndef AUTH_EXTERNAL_AUTH_H
#define AUTH_EXTERNAL_AUTH_H

#include "auth/dispatcher.h"
#include "common/error.h"

/*
 * CONTRACT.mdセクション11「認証: Client Credentials Grant」(backend(Go)の
 * RequireExternalClientAuth・backend-cpp/backend-rustの同名ロジックに相当)。
 * 内部REST/gRPC用のauth_resolve_user_id(auth/user_resolver.h)とは検証内容が異なる
 * ため別関数にしている:
 *   - ローカルHMAC/RSA発行のJWTは拒否する(Keycloak発行のみ許可)
 *   - user_id解決は行わない(呼び出し元がクエリパラメータのuser_idをそのまま使う、
 *     CONTRACT.md記載の既知の設計。クライアント資格情報を持つ者は任意ユーザーの
 *     タスクを読み取れる)
 *   - 代わりにazp(authorized party)クレームがexternal_api_client_idと一致することを
 *     必須とする
 * 戻り値: 成功時TASK_OK、失敗時TASK_ERR_UNAUTHORIZED(401 {"error":"unauthenticated"})
 */
TaskError auth_require_external_client(Dispatcher *dispatcher, const char *authorization_header,
                                        const char *external_api_client_id);

#endif /* AUTH_EXTERNAL_AUTH_H */
