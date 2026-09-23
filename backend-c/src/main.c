#include <civetweb.h>
#include <mysql/mysql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "auth/auth_module.h"
#include "common/log.h"
#include "db/mysql_conn.h"
#include "external/external_handler.h"
#include "flags/feature_flag_poller.h"
#include "grpc/grpc_server.h"
#include "http/handler.h"

static const char *env_or(const char *name, const char *fallback) {
    const char *v = getenv(name);
    return v != NULL ? v : fallback;
}

int main(void) {
    /*
     * 【ログ出力のバッド/グッドプラクティス】標準出力がターミナルではなく
     * ファイル/パイプへリダイレクトされている場合(`> server.log`等)、Cの標準ライブラリは
     * 既定でstdoutを「完全バッファリング」する。これを何もせず放置すると、以下のように
     * printfを呼んでも実際にはメモリ上のバッファに積まれるだけで、バッファが一杯になる
     * かプロセスが終了するまでファイルには一切書き出されない(tail -fで追跡しても
     * 何も見えない、CWE-1076相当の「観測可能性の欠如」):
     *
     *   printf("rest method=... status=...\n");  // ← バッファリングされ、即時には出ない
     *
     * 修正後: setvbufで明示的に「改行ごとにフラッシュする」行バッファリングへ切り替える。
     * これによりgrpc/rest/external各ハンドラのリクエスト単位ログが、発生した瞬間に
     * ファイル/パイプへ書き出されるようになる */
    setvbuf(stdout, NULL, _IOLBF, 0);

    /* LOG_LEVEL=debugのときだけ各モジュールのlog_debugfが出力される
     * (README.md「各種ログの出力先」節参照)。他の初期化より前に呼ぶ必要がある */
    log_module_init();

    /* backend-cpp/src/config.cppと同じ環境変数名(HTTP_ADDR/GRPC_ADDRはポート番号のみ、
     * DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_SCHEMA)。JWT/JWKS関連もbackend-rust
     * (src/config.rs)・backend-cpp(src/config.hpp)と同じ環境変数名・既定値にそろえている */
    const char *http_addr = env_or("HTTP_ADDR", "8106");
    const char *grpc_port = env_or("GRPC_ADDR", "9100");
    const char *external_http_addr = env_or("EXTERNAL_HTTP_ADDR", "8110");
    const char *external_api_client_id = env_or("EXTERNAL_API_CLIENT_ID", "external-api-client");
    const char *db_host = env_or("DB_HOST", "127.0.0.1");
    unsigned int db_port = (unsigned int)atoi(env_or("DB_PORT", "13306"));
    const char *db_user = env_or("DB_USER", "root");
    const char *db_password = env_or("DB_PASSWORD", "");
    const char *db_schema = env_or("DB_SCHEMA", "bff_gin_development");

    const char *keycloak_issuer =
        env_or("KEYCLOAK_ISSUER", "http://localhost:8082/realms/training");
    const char *expected_audience = env_or("EXPECTED_AUDIENCE", "backend");
    const char *local_hmac_secret =
        env_or("LOCAL_AUTH_HMAC_SECRET", "local-dev-hmac-shared-secret-change-me");
    const char *local_rsa_jwks_url =
        env_or("LOCAL_AUTH_RSA_JWKS_URL", "http://localhost:8080/.well-known/jwks.json");
    /* KEYCLOAK_JWKS_URLが明示されなければKEYCLOAK_ISSUERから導出する
     * (backend-rust/src/config.rsのkeycloak_jwks_urlと同じ既定値の作り方) */
    char default_keycloak_jwks_url[512];
    snprintf(default_keycloak_jwks_url, sizeof(default_keycloak_jwks_url),
             "%s/protocol/openid-connect/certs", keycloak_issuer);
    const char *keycloak_jwks_url = env_or("KEYCLOAK_JWKS_URL", default_keycloak_jwks_url);

    printf("backend-c starting: HTTP_ADDR=:%s db=%s:%u/%s\n", http_addr, db_host, db_port,
           db_schema);

    if (mysql_conn_module_init(db_host, db_port, db_user, db_password, db_schema) != 0) {
        fprintf(stderr, "mysql_conn_module_init failed\n");
        return 1;
    }

    if (auth_module_init(keycloak_issuer, keycloak_jwks_url, expected_audience,
                          local_hmac_secret, local_rsa_jwks_url, external_api_client_id) != 0) {
        fprintf(stderr, "auth_module_init failed\n");
        return 1;
    }

    /* backend.external-tasks-pagination-v2の判定用(外部公開APIのみが参照する)。
     * bffのようなHTTPポーリングではなく、feature_flagsテーブルを直接ポーリングする
     * (README.md「Feature Flag」節参照) */
    if (feature_flag_poller_start() != 0) {
        fprintf(stderr, "feature_flag_poller_start failed\n");
        return 1;
    }

    mg_init_library(0);

    const char *options[] = {"listening_ports", http_addr, "num_threads", "16", NULL};
    struct mg_context *ctx = mg_start(NULL, NULL, options);
    if (ctx == NULL) {
        fprintf(stderr, "mg_start failed (port %s in use?)\n", http_addr);
        return 1;
    }

    /* "/internal/v1/tasks"1つだけに登録し、コレクション/個別リソースの振り分けは
     * task_request_handler内部で行う(handler.h参照。CivetWebのハンドラマッチングの
     * 既知の挙動により、素朴に2パターン登録すると個別リソース側に永遠に到達しない) */
    mg_set_request_handler(ctx, "/internal/v1/tasks", task_request_handler, NULL);

    /* gRPC v2(gRPC Core C API直叩き、README.md「gRPC」節参照)。CivetWeb同様、
     * grpc_server_module_start自身が内部にワーカースレッドを持つため、
     * この呼び出しはブロックせずすぐ戻る */
    char grpc_listen_addr[64];
    snprintf(grpc_listen_addr, sizeof(grpc_listen_addr), "0.0.0.0:%s", grpc_port);
    if (grpc_server_module_start(grpc_listen_addr) != 0) {
        fprintf(stderr, "grpc_server_module_start failed (port %s in use?)\n", grpc_port);
        return 1;
    }

    /* 外部公開API(CONTRACT.mdセクション11)専用の3つ目のリスナー。内部REST v1とは
     * 認証モデル(Client Credentials Grant限定・azp検証)が異なるため、パスを追加するの
     * ではなく独立したmg_context(別ポート)を新設する(backend-cpp/backend-rustと同じ
     * 構成)。CivetWebは1プロセス内に複数の独立したmg_contextを持てる */
    const char *external_options[] = {"listening_ports", external_http_addr, "num_threads", "8",
                                       NULL};
    struct mg_context *external_ctx = mg_start(NULL, NULL, external_options);
    if (external_ctx == NULL) {
        fprintf(stderr, "mg_start(external) failed (port %s in use?)\n", external_http_addr);
        return 1;
    }
    mg_set_request_handler(external_ctx, "/external/v1/tasks", external_task_request_handler,
                            NULL);

    printf("backend-c listening: REST=:%s GRPC=:%s EXTERNAL=:%s\n", http_addr, grpc_port,
           external_http_addr);
    printf("press Ctrl+C to stop\n");

    /* CivetWebはmg_startの時点で固定サイズのワーカースレッドプールを起動済み
     * (thread-poolモデル、README.md「アーキテクチャ選定」節参照)。メインスレッドは
     * シグナル待ちだけを行う */
    for (;;) {
        sleep(1);
    }

    feature_flag_poller_stop();
    mg_stop(external_ctx);
    mg_stop(ctx);
    mg_exit_library();
    return 0;
}
