#ifndef AUTH_AUTH_MODULE_H
#define AUTH_AUTH_MODULE_H

#include "auth/dispatcher.h"

/*
 * db/mysql_conn.hのmysql_conn_module_initと同じ「モジュール単位のグローバルシングルトン」
 * パターン。CivetWeb(src/http/handler.c)・gRPC Core(src/grpc/grpc_server.c)のハンドラは
 * どちらも関数ポインタで登録するプレーンなC関数であり、依存性注入の自然な置き場が無いため、
 * プロセス起動時に一度だけ初期化したDispatcher*をここに保持して両トランスポートから
 * 共有する
 *
 * プロセス起動時(main関数)に一度だけ呼ぶこと。戻り値: 成功時0、失敗時-1
 */
int auth_module_init(const char *keycloak_issuer, const char *keycloak_jwks_url,
                      const char *expected_audience, const char *local_hmac_secret,
                      const char *local_rsa_jwks_url, const char *external_api_client_id);

/* auth_module_init()呼び出し前はNULLを返す(dispatcher_verify/dispatcher_registerは
 * NULLを渡されても安全に失敗するため、結合テストのように一部だけ初期化する用途でも壊れない) */
Dispatcher *auth_module_dispatcher(void);

/* 外部公開API(src/external/external_handler.c)のazp一致チェック用。
 * auth_module_init()呼び出し前は空文字列を返す */
const char *auth_module_external_api_client_id(void);

#endif /* AUTH_AUTH_MODULE_H */
