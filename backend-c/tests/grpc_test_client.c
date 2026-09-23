#include "grpc_test_client.h"

#include <grpc/byte_buffer.h>
#include <grpc/byte_buffer_reader.h>
#include <grpc/credentials.h>
#include <grpc/slice.h>
#include <grpc/support/alloc.h>
#include <grpc/support/time.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int grpc_test_client_init(GrpcTestClient *client, const char *target) {
    grpc_channel_credentials *creds = grpc_insecure_credentials_create();
    client->channel = grpc_channel_create(target, creds, NULL);
    grpc_channel_credentials_release(creds);
    if (client->channel == NULL) return -1;
    client->cq = grpc_completion_queue_create_for_next(NULL);
    return 0;
}

void grpc_test_client_destroy(GrpcTestClient *client) {
    if (client->channel != NULL) grpc_channel_destroy(client->channel);
    if (client->cq != NULL) {
        grpc_completion_queue_shutdown(client->cq);
        /* GRPC_QUEUE_SHUTDOWNが返るまでドレインする(destroy前に必須、grpc.hの規約) */
        for (;;) {
            grpc_event event =
                grpc_completion_queue_next(client->cq, gpr_inf_future(GPR_CLOCK_MONOTONIC), NULL);
            if (event.type == GRPC_QUEUE_SHUTDOWN) break;
        }
        grpc_completion_queue_destroy(client->cq);
    }
    client->channel = NULL;
    client->cq = NULL;
}

void grpc_test_client_call(GrpcTestClient *client, const char *method, const char *bearer_token,
                            const uint8_t *request_bytes, size_t request_len,
                            GrpcTestCallResult *result) {
    memset(result, 0, sizeof(*result));

    gpr_timespec deadline =
        gpr_time_add(gpr_now(GPR_CLOCK_MONOTONIC), gpr_time_from_seconds(5, GPR_TIMESPAN));
    grpc_slice method_slice = grpc_slice_from_copied_string(method);
    grpc_call *call = grpc_channel_create_call(client->channel, NULL, GRPC_PROPAGATE_DEFAULTS,
                                                client->cq, method_slice, NULL, deadline, NULL);
    grpc_slice_unref(method_slice);
    if (call == NULL) {
        result->status = GRPC_STATUS_INTERNAL;
        result->status_message = strdup("grpc_channel_create_call failed");
        return;
    }

    grpc_metadata auth_metadata;
    char *auth_value = NULL;
    memset(&auth_metadata, 0, sizeof(auth_metadata));
    if (bearer_token != NULL) {
        size_t auth_value_len = strlen("Bearer ") + strlen(bearer_token) + 1;
        auth_value = (char *)malloc(auth_value_len);
        snprintf(auth_value, auth_value_len, "Bearer %s", bearer_token);
        auth_metadata.key = grpc_slice_from_static_string("authorization");
        auth_metadata.value = grpc_slice_from_copied_string(auth_value);
    }

    grpc_slice request_slice =
        grpc_slice_from_copied_buffer((const char *)request_bytes, request_len);
    grpc_byte_buffer *request_bb = grpc_raw_byte_buffer_create(&request_slice, 1);
    grpc_slice_unref(request_slice);

    grpc_metadata_array recv_initial_metadata;
    grpc_metadata_array recv_trailing_metadata;
    grpc_metadata_array_init(&recv_initial_metadata);
    grpc_metadata_array_init(&recv_trailing_metadata);
    grpc_byte_buffer *response_bb = NULL;
    grpc_status_code status = GRPC_STATUS_UNKNOWN;
    grpc_slice status_details = grpc_empty_slice();

    /*
     * 1回のunary呼び出しをsend側/recv側まとめて1バッチにする(サーバー側grpc_server.cとは
     * 異なり、送信後すぐ受信を待つだけなので複数バッチに分ける必要が無い。全ops完了で
     * 1回だけcompletion queueにイベントが積まれる) */
    grpc_op ops[6];
    memset(ops, 0, sizeof(ops));
    size_t nops = 0;

    ops[nops].op = GRPC_OP_SEND_INITIAL_METADATA;
    ops[nops].data.send_initial_metadata.count = bearer_token != NULL ? 1 : 0;
    ops[nops].data.send_initial_metadata.metadata = bearer_token != NULL ? &auth_metadata : NULL;
    nops++;

    ops[nops].op = GRPC_OP_SEND_MESSAGE;
    ops[nops].data.send_message.send_message = request_bb;
    nops++;

    ops[nops].op = GRPC_OP_SEND_CLOSE_FROM_CLIENT;
    nops++;

    ops[nops].op = GRPC_OP_RECV_INITIAL_METADATA;
    ops[nops].data.recv_initial_metadata.recv_initial_metadata = &recv_initial_metadata;
    nops++;

    ops[nops].op = GRPC_OP_RECV_MESSAGE;
    ops[nops].data.recv_message.recv_message = &response_bb;
    nops++;

    ops[nops].op = GRPC_OP_RECV_STATUS_ON_CLIENT;
    ops[nops].data.recv_status_on_client.trailing_metadata = &recv_trailing_metadata;
    ops[nops].data.recv_status_on_client.status = &status;
    ops[nops].data.recv_status_on_client.status_details = &status_details;
    ops[nops].data.recv_status_on_client.error_string = NULL;
    nops++;

    int tag = 1;
    grpc_call_error err = grpc_call_start_batch(call, ops, nops, &tag, NULL);
    grpc_byte_buffer_destroy(request_bb);
    if (bearer_token != NULL) {
        grpc_slice_unref(auth_metadata.key);
        grpc_slice_unref(auth_metadata.value);
        free(auth_value);
    }
    if (err != GRPC_CALL_OK) {
        result->status = GRPC_STATUS_INTERNAL;
        result->status_message = strdup("grpc_call_start_batch failed");
        grpc_metadata_array_destroy(&recv_initial_metadata);
        grpc_metadata_array_destroy(&recv_trailing_metadata);
        grpc_call_unref(call);
        return;
    }

    grpc_event event = grpc_completion_queue_next(client->cq, gpr_inf_future(GPR_CLOCK_MONOTONIC), NULL);
    (void)event; /* tag=1のみ使っているため、この単純なクライアントでは中身の確認は不要 */

    result->status = status;
    result->status_message = grpc_slice_to_c_string(status_details);
    /* grpc_slice_to_c_stringはgpr_malloc()で確保する(grpc_server.cのresolve_user_id_from_metadataの
     * コメント参照)ため、そのままresult->status_messageに保持しても構わないが、
     * このクライアントの解放関数(grpc_test_call_result_destroy)はfree()を使う統一設計に
     * したいので、ここでstrdup()し直してからgpr_free()する */
    if (result->status_message != NULL) {
        char *copy = strdup(result->status_message);
        gpr_free(result->status_message);
        result->status_message = copy;
    }

    if (status == GRPC_STATUS_OK && response_bb != NULL) {
        grpc_byte_buffer_reader reader;
        if (grpc_byte_buffer_reader_init(&reader, response_bb)) {
            grpc_slice full = grpc_byte_buffer_reader_readall(&reader);
            size_t len = GRPC_SLICE_LENGTH(full);
            result->response_bytes = (uint8_t *)malloc(len > 0 ? len : 1);
            if (result->response_bytes != NULL) {
                memcpy(result->response_bytes, GRPC_SLICE_START_PTR(full), len);
                result->response_len = len;
            }
            grpc_slice_unref(full);
            grpc_byte_buffer_reader_destroy(&reader);
        }
    }

    if (response_bb != NULL) grpc_byte_buffer_destroy(response_bb);
    grpc_slice_unref(status_details);
    grpc_metadata_array_destroy(&recv_initial_metadata);
    grpc_metadata_array_destroy(&recv_trailing_metadata);
    grpc_call_unref(call);
}

void grpc_test_call_result_destroy(GrpcTestCallResult *result) {
    free(result->status_message);
    free(result->response_bytes);
    result->status_message = NULL;
    result->response_bytes = NULL;
}
