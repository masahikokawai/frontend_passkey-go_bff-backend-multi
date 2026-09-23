#ifndef GRPC_GRPC_SERVER_H
#define GRPC_GRPC_SERVER_H

/*
 * gRPC v2サーバー(gRPC Core C API直叩き、README.md「gRPC」節参照)。
 * addr例: "0.0.0.0:9100"。成功時0、失敗時-1を返す。
 * 内部でワーカースレッドを起動して即座に戻る(CivetWebのthread-poolモデルと同様、
 * REST/gRPCどちらも「呼び出し元スレッドはブロックしない」設計に揃えている)
 */
int grpc_server_module_start(const char *addr);

/*
 * サーバーを正常停止し、gRPC Coreのリソース(server/completion queue/grpc_init参照カウント)を
 * 全て解放する。backend_c_grpc_integration_tests(結合テスト)がプロセス終了前に呼ぶ想定
 * (AddressSanitizerでリークとして検出されないようにするため、REST/main.cのように
 * プロセスをkillするだけで済ませない)
 */
void grpc_server_module_stop(void);

#endif /* GRPC_GRPC_SERVER_H */
