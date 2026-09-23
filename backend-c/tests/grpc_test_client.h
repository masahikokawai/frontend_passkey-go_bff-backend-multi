#ifndef TESTS_GRPC_TEST_CLIENT_H
#define TESTS_GRPC_TEST_CLIENT_H

#include <grpc/grpc.h>
#include <stddef.h>
#include <stdint.h>

/*
 * 結合テスト用の最小限の同期gRPCクライアント。backend-c自身がRPCスタブ生成を持たない
 * (protobuf-cはメッセージのみ生成、CMakeLists.txt参照)のと同じ理由で、クライアント側も
 * gRPC Core C APIを直接使って自作する(サーバー側src/grpc/grpc_server.cと対になる実装。
 * 1呼び出し=1バッチ(SEND_INITIAL_METADATA+SEND_MESSAGE+SEND_CLOSE_FROM_CLIENT+
 * RECV_INITIAL_METADATA+RECV_MESSAGE+RECV_STATUS_ON_CLIENT)にまとめ、
 * 呼び出しスレッド自身でcompletion queueを1回pollするだけの単純な同期実装にしている
 */
typedef struct {
    grpc_channel *channel;
    grpc_completion_queue *cq;
} GrpcTestClient;

typedef struct {
    grpc_status_code status;
    char *status_message;      /* 呼び出し側がfree()すること(NULLの場合あり) */
    uint8_t *response_bytes;   /* 呼び出し側がfree()すること。status!=OKならNULL */
    size_t response_len;
} GrpcTestCallResult;

/* target例: "127.0.0.1:9100" */
int grpc_test_client_init(GrpcTestClient *client, const char *target);
void grpc_test_client_destroy(GrpcTestClient *client);

/*
 * method例: "/task.v1.TaskService/GetTask"。bearer_tokenがNULLの場合はauthorization
 * メタデータを付けずに呼ぶ(未認証呼び出しのテスト用)。それ以外の場合は
 * "authorization: Bearer <bearer_token>"メタデータを1件付けて呼ぶ(JWT本実装、
 * README.md「認証について」参照)
 */
void grpc_test_client_call(GrpcTestClient *client, const char *method, const char *bearer_token,
                            const uint8_t *request_bytes, size_t request_len,
                            GrpcTestCallResult *result);
void grpc_test_call_result_destroy(GrpcTestCallResult *result);

#endif /* TESTS_GRPC_TEST_CLIENT_H */
