#include "grpc/grpc_server.h"

#include <grpc/byte_buffer.h>
#include <grpc/byte_buffer_reader.h>
#include <grpc/credentials.h>
#include <grpc/grpc.h>
#include <grpc/slice.h>
#include <grpc/support/alloc.h>
#include <grpc/support/time.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "auth/auth_module.h"
#include "auth/user_resolver.h"
#include "grpc/task_grpc_handlers.h"
#include "task/v1/task.pb-c.h"

/*
 * gRPC Core C API(grpc/grpc.h)を直接使い、gRPC自体が提供するC言語スタブ生成
 * (protoc --grpc_out相当)を使わずにサーバーを実装する。
 * 【背景】公式protocにはC言語のコード生成ターゲットが無く、protobuf-c(protoc-gen-c)は
 * メッセージのシリアライズのみを生成する(RPCスタブ生成機能は無い、CMakeLists.txt
 * 「protobuf-cの既知の制約」節参照)。そのためRPCの受付・メッセージ送受信・
 * メソッド名によるディスパッチを全て手動で行う必要がある
 * (README.md「gRPC」節、低レイヤーのcompletion queueエンジンについての解説を参照)
 */

#define GRPC_SERVER_WORKER_THREADS 4

typedef enum {
    RPC_LIST_TASKS = 0,
    RPC_GET_TASK,
    RPC_CREATE_TASK,
    RPC_UPDATE_TASK,
    RPC_DELETE_TASK,
    RPC_METHOD_COUNT
} RpcMethod;

static const char *kMethodPaths[RPC_METHOD_COUNT] = {
    "/task.v1.TaskService/ListTasks", "/task.v1.TaskService/GetTask",
    "/task.v1.TaskService/CreateTask", "/task.v1.TaskService/UpdateTask",
    "/task.v1.TaskService/DeleteTask",
};

typedef enum { PHASE_NEW_CALL, PHASE_FINISH, PHASE_CLOSE } CallPhase;

typedef struct CallState CallState;

/*
 * 【tagについての設計判断】grpc_call_start_batchのtagはvoid*であり、1つのcallに対して
 * 複数のバッチ(このRPCではFINISHとCLOSEの2つ)が同時に未完了でありうる。
 * 同じCallState*をそのままtagにすると、completion queueから返ってきたtagだけでは
 * どちらのバッチが完了したのか区別できない。バッチごとに専用のOpTag(所属するCallStateへの
 * ポインタ+フェーズ)をCallStateに埋め込み、そのOpTagのアドレスをtagとして渡すことで
 * 一意に区別する(grpc-core自体のテストコードでも使われる標準的な手法)
 */
typedef struct {
    CallState *cs;
    CallPhase phase;
} OpTag;

struct CallState {
    RpcMethod method;
    grpc_call *call;
    grpc_call_details call_details;
    grpc_metadata_array request_metadata;
    grpc_byte_buffer *request_payload;  /* PHASE_NEW_CALL到達時に自動で受信済み */
    grpc_byte_buffer *response_payload; /* FINISHバッチで送信するため保持する */
    int cancelled;
    int done_count; /* FINISHとCLOSEの両方が来たらcall_state_destroy可能(=2) */
    struct timespec start_time;
    OpTag tag_new_call;
    OpTag tag_finish;
    OpTag tag_close;
};

static grpc_server *g_server = NULL;
static grpc_completion_queue *g_cq = NULL;
static void *g_method_handles[RPC_METHOD_COUNT];
static pthread_t g_workers[GRPC_SERVER_WORKER_THREADS];
static int g_worker_count = 0;
static OpTag g_shutdown_tag = {NULL, PHASE_CLOSE}; /* phaseは使わない、tag識別専用 */

static CallState *call_state_create(RpcMethod method) {
    CallState *cs = (CallState *)calloc(1, sizeof(CallState));
    if (cs == NULL) return NULL;
    cs->method = method;
    grpc_call_details_init(&cs->call_details);
    grpc_metadata_array_init(&cs->request_metadata);
    cs->tag_new_call.cs = cs;
    cs->tag_new_call.phase = PHASE_NEW_CALL;
    cs->tag_finish.cs = cs;
    cs->tag_finish.phase = PHASE_FINISH;
    cs->tag_close.cs = cs;
    cs->tag_close.phase = PHASE_CLOSE;
    return cs;
}

static void call_state_destroy(CallState *cs) {
    if (cs == NULL) return;
    grpc_call_details_destroy(&cs->call_details);
    grpc_metadata_array_destroy(&cs->request_metadata);
    if (cs->request_payload != NULL) grpc_byte_buffer_destroy(cs->request_payload);
    if (cs->response_payload != NULL) grpc_byte_buffer_destroy(cs->response_payload);
    if (cs->call != NULL) grpc_call_unref(cs->call);
    free(cs);
}

/* 次の新規呼び出しを1件、事前に受け付け登録する(再アーム)。
 * 【この学習用実装での簡略化】メソッドごとに常に1件だけ事前登録する
 * (backend-cppのgRPC実装同様、極端な高同時実行数は想定していない。README.md参照)。
 * 呼び出し中の別のCallStateとは独立な新しいCallStateを使うため、
 * 処理中のRPCを妨げずに次の着信を受け付けられる */
static void request_new_call(RpcMethod method) {
    CallState *cs = call_state_create(method);
    if (cs == NULL) {
        fprintf(stderr, "grpc: call_state_create failed (OOM), method=%s not re-armed\n",
                kMethodPaths[method]);
        return;
    }
    grpc_call_error err = grpc_server_request_registered_call(
        g_server, g_method_handles[method], &cs->call, &cs->call_details.deadline,
        &cs->request_metadata, &cs->request_payload, g_cq, g_cq, &cs->tag_new_call);
    if (err != GRPC_CALL_OK) {
        fprintf(stderr, "grpc: grpc_server_request_registered_call failed: %d\n", (int)err);
        call_state_destroy(cs);
    }
}

/*
 * JWT/JWKS本実装(auth/dispatcher.h・auth/user_resolver.h)へ委譲する。REST側
 * (src/http/handler.cのresolve_user_id)と同じauth_resolve_user_idを使うことで、
 * 認証ロジックをトランスポートごとに複製しない。gRPCの"authorization"メタデータ
 * ("Bearer xxx"形式、HTTPのAuthorizationヘッダと同じ慣習)を渡す */
static TaskError resolve_user_id_from_metadata(const grpc_metadata_array *metadata,
                                                int64_t *out_user_id) {
    for (size_t i = 0; i < metadata->count; i++) {
        if (grpc_slice_str_cmp(metadata->metadata[i].key, "authorization") != 0) continue;

        /*
         * 【メモリ管理のバッド/グッドプラクティス】grpc_slice_to_c_stringはgpr_malloc()で
         * 確保されたバッファを返す(grpc/slice.hのドキュメントコメント参照)。以下のように
         * 通常のfree()で解放すると、gprのアロケータとlibcのアロケータの実装が食い違う
         * ビルド構成では未定義動作になる(異なるアロケータ間でポインタを混用する典型的バグ):
         *
         *   char *value = grpc_slice_to_c_string(metadata->metadata[i].value);
         *   ...
         *   free(value);  // ← gpr_malloc()で確保されたものをfree()で解放してしまう
         *
         * 修正後: gpr_free()で解放する(gpr_malloc/gpr_freeは常にペアで使う) */
        char *value = grpc_slice_to_c_string(metadata->metadata[i].value);
        if (value == NULL) return TASK_ERR_UNAUTHORIZED;
        TaskError err = auth_resolve_user_id(auth_module_dispatcher(), value, out_user_id);
        gpr_free(value);
        return err;
    }
    /* "authorization"メタデータ自体が無い場合(NULLを渡すとauth_resolve_user_idは
     * 即座にTASK_ERR_UNAUTHORIZEDを返す) */
    return auth_resolve_user_id(auth_module_dispatcher(), NULL, out_user_id);
}

static grpc_status_code task_error_to_grpc_code(TaskError err) {
    switch (err) {
        case TASK_ERR_NOT_FOUND:
            return GRPC_STATUS_NOT_FOUND;
        case TASK_ERR_UNAUTHORIZED:
            return GRPC_STATUS_UNAUTHENTICATED;
        case TASK_ERR_DB_ERROR:
        case TASK_ERR_MEMORY_ERROR:
            return GRPC_STATUS_INTERNAL;
        case TASK_ERR_INVALID_REQUEST:
        case TASK_ERR_INVALID_ID:
        case TASK_ERR_INVALID_STATUS:
        case TASK_ERR_INVALID_FINISHED_ON:
        case TASK_ERR_VALIDATION_ERROR:
        default:
            return GRPC_STATUS_INVALID_ARGUMENT;
    }
}

/* Task__V1__Taskをpackしてgrpc_byte_bufferへ包む。呼び出し側がgrpc_byte_buffer_destroyすること
 * (task自体はfree_unpacked済みでよい、pack()はバイト列にシリアライズするだけなので) */
static grpc_byte_buffer *pack_task(const Task__V1__Task *task) {
    size_t size = task__v1__task__get_packed_size(task);
    uint8_t *buf = (uint8_t *)malloc(size > 0 ? size : 1);
    if (buf == NULL) return NULL;
    task__v1__task__pack(task, buf);
    /*
     * grpc_slice_from_copied_bufferはbufの内容を複製したスライスを返すため、
     * このあとbufを即free()してよい(スライス自体はgrpc-core内部の参照カウントで
     * 管理される)。grpc_raw_byte_buffer_createはslices配列の各要素の参照カウントを
     * 自ら1つ増やして保持するため、直後にこちらの手持ち分をgrpc_slice_unrefして
     * 手放す(そうしないと、このスライスはプロセス終了までゼロにならずリークする) */
    grpc_slice slice = grpc_slice_from_copied_buffer((const char *)buf, size);
    free(buf);
    grpc_byte_buffer *bb = grpc_raw_byte_buffer_create(&slice, 1);
    grpc_slice_unref(slice);
    return bb;
}

static grpc_byte_buffer *pack_list_tasks_response(const Task__V1__ListTasksResponse *resp) {
    size_t size = task__v1__list_tasks_response__get_packed_size(resp);
    uint8_t *buf = (uint8_t *)malloc(size > 0 ? size : 1);
    if (buf == NULL) return NULL;
    task__v1__list_tasks_response__pack(resp, buf);
    grpc_slice slice = grpc_slice_from_copied_buffer((const char *)buf, size);
    free(buf);
    grpc_byte_buffer *bb = grpc_raw_byte_buffer_create(&slice, 1);
    grpc_slice_unref(slice);
    return bb;
}

static grpc_byte_buffer *pack_delete_response(void) {
    Task__V1__DeleteTaskResponse resp = TASK__V1__DELETE_TASK_RESPONSE__INIT;
    size_t size = task__v1__delete_task_response__get_packed_size(&resp);
    uint8_t *buf = (uint8_t *)malloc(size > 0 ? size : 1);
    if (buf == NULL) return NULL;
    task__v1__delete_task_response__pack(&resp, buf);
    grpc_slice slice = grpc_slice_from_copied_buffer((const char *)buf, size);
    free(buf);
    grpc_byte_buffer *bb = grpc_raw_byte_buffer_create(&slice, 1);
    grpc_slice_unref(slice);
    return bb;
}

/* request_payloadを1つの連続バイト列として取り出す。戻り値はgrpc_slice_unrefすること
 * (中身が空の呼び出し(request_payload==NULL)ならgrpc_empty_sliceを返す) */
static grpc_slice read_request_payload(grpc_byte_buffer *payload) {
    if (payload == NULL) return grpc_empty_slice();
    grpc_byte_buffer_reader reader;
    if (!grpc_byte_buffer_reader_init(&reader, payload)) return grpc_empty_slice();
    grpc_slice slice = grpc_byte_buffer_reader_readall(&reader);
    grpc_byte_buffer_reader_destroy(&reader);
    return slice;
}

/*
 * 1件のRPCを実際に処理する(認証・unpack・Repository呼び出し・pack)。
 * 成功時はcs->response_payloadを設定してGRPC_STATUS_OKを返す。
 * 失敗時はcs->response_payloadをNULLのままにし、status_messageに静的またはmalloc済みの
 * メッセージを設定する(*message_ownedが1ならout_messageは呼び出し側がfree()すること) */
static grpc_status_code process_call(CallState *cs, const char **out_message, int *message_owned) {
    *message_owned = 0;

    int64_t user_id = 0;
    if (resolve_user_id_from_metadata(&cs->request_metadata, &user_id) != TASK_OK) {
        *out_message = "unauthorized";
        return GRPC_STATUS_UNAUTHENTICATED;
    }

    grpc_slice payload = read_request_payload(cs->request_payload);
    const uint8_t *data = GRPC_SLICE_START_PTR(payload);
    size_t len = GRPC_SLICE_LENGTH(payload);

    grpc_status_code code = GRPC_STATUS_OK;
    char *validation_message = NULL;

    switch (cs->method) {
        case RPC_LIST_TASKS: {
            Task__V1__ListTasksRequest *req = task__v1__list_tasks_request__unpack(NULL, len, data);
            if (req == NULL) {
                *out_message = "invalid request body";
                code = GRPC_STATUS_INVALID_ARGUMENT;
                break;
            }
            Task__V1__ListTasksResponse *resp = NULL;
            TaskError err = grpc_handle_list_tasks(user_id, req, &resp);
            task__v1__list_tasks_request__free_unpacked(req, NULL);
            if (err != TASK_OK) {
                code = task_error_to_grpc_code(err);
                *out_message = "list_tasks failed";
                break;
            }
            cs->response_payload = pack_list_tasks_response(resp);
            task__v1__list_tasks_response__free_unpacked(resp, NULL);
            break;
        }
        case RPC_GET_TASK: {
            Task__V1__GetTaskRequest *req = task__v1__get_task_request__unpack(NULL, len, data);
            if (req == NULL) {
                *out_message = "invalid request body";
                code = GRPC_STATUS_INVALID_ARGUMENT;
                break;
            }
            Task__V1__Task *resp = NULL;
            TaskError err = grpc_handle_get_task(user_id, req, &resp);
            task__v1__get_task_request__free_unpacked(req, NULL);
            if (err != TASK_OK) {
                code = task_error_to_grpc_code(err);
                *out_message = (err == TASK_ERR_NOT_FOUND) ? "task not found" : "get_task failed";
                break;
            }
            cs->response_payload = pack_task(resp);
            task__v1__task__free_unpacked(resp, NULL);
            break;
        }
        case RPC_CREATE_TASK: {
            Task__V1__CreateTaskRequest *req =
                task__v1__create_task_request__unpack(NULL, len, data);
            if (req == NULL) {
                *out_message = "invalid request body";
                code = GRPC_STATUS_INVALID_ARGUMENT;
                break;
            }
            Task__V1__Task *resp = NULL;
            TaskError err = grpc_handle_create_task(user_id, req, &resp, &validation_message);
            task__v1__create_task_request__free_unpacked(req, NULL);
            if (err != TASK_OK) {
                code = task_error_to_grpc_code(err);
                *out_message = validation_message != NULL ? validation_message : "create_task failed";
                *message_owned = validation_message != NULL;
                break;
            }
            cs->response_payload = pack_task(resp);
            task__v1__task__free_unpacked(resp, NULL);
            break;
        }
        case RPC_UPDATE_TASK: {
            Task__V1__UpdateTaskRequest *req =
                task__v1__update_task_request__unpack(NULL, len, data);
            if (req == NULL) {
                *out_message = "invalid request body";
                code = GRPC_STATUS_INVALID_ARGUMENT;
                break;
            }
            Task__V1__Task *resp = NULL;
            TaskError err = grpc_handle_update_task(user_id, req, &resp, &validation_message);
            task__v1__update_task_request__free_unpacked(req, NULL);
            if (err != TASK_OK) {
                code = task_error_to_grpc_code(err);
                *out_message = validation_message != NULL ? validation_message
                               : (err == TASK_ERR_NOT_FOUND) ? "task not found"
                                                              : "update_task failed";
                *message_owned = validation_message != NULL;
                break;
            }
            cs->response_payload = pack_task(resp);
            task__v1__task__free_unpacked(resp, NULL);
            break;
        }
        case RPC_DELETE_TASK: {
            Task__V1__DeleteTaskRequest *req =
                task__v1__delete_task_request__unpack(NULL, len, data);
            if (req == NULL) {
                *out_message = "invalid request body";
                code = GRPC_STATUS_INVALID_ARGUMENT;
                break;
            }
            TaskError err = grpc_handle_delete_task(user_id, req);
            task__v1__delete_task_request__free_unpacked(req, NULL);
            if (err != TASK_OK) {
                code = task_error_to_grpc_code(err);
                *out_message = (err == TASK_ERR_NOT_FOUND) ? "task not found" : "delete_task failed";
                break;
            }
            cs->response_payload = pack_delete_response();
            break;
        }
        default:
            code = GRPC_STATUS_UNIMPLEMENTED;
            *out_message = "unknown method";
            break;
    }

    grpc_slice_unref(payload);

    if (code == GRPC_STATUS_OK && cs->response_payload == NULL) {
        /* pack_*がmalloc失敗した場合のみここに到達する */
        code = GRPC_STATUS_INTERNAL;
        *out_message = "internal error";
    }
    return code;
}

/* method/実際のgRPCステータス/durationを1rpc1行のログとして出す
 * (backend-cppのLogRpc・他言語のリクエスト単位ログと同じ設計、README.md参照) */
static void log_rpc(RpcMethod method, const struct timespec *start, grpc_status_code status) {
    struct timespec end;
    clock_gettime(CLOCK_MONOTONIC, &end);
    long duration_ms = (end.tv_sec - start->tv_sec) * 1000 +
                        (end.tv_nsec - start->tv_nsec) / 1000000;
    printf("grpc method=%s status=%d duration_ms=%ld\n", kMethodPaths[method], (int)status,
           duration_ms);
}

static void handle_new_call(CallState *cs) {
    clock_gettime(CLOCK_MONOTONIC, &cs->start_time);

    /* 次の着信を先に受け付けておく(このRPCの処理時間に関わらず、後続の呼び出しを
     * 待たせないため。冒頭のrequest_new_callのコメント参照) */
    request_new_call(cs->method);

    /* RECV_CLOSE_ON_SERVERは呼び出し全体が終わった後に完了する(cancel検知用)。
     * FINISHバッチとは別のtag(tag_close)で独立に投げる必要がある
     * (両方を同じバッチに入れるとRECV_CLOSE_ON_SERVERがFINISH送信後にしか完了しないため、
     * バッチ全体の完了通知そのものが遅れてしまう。ファイル冒頭のOpTagのコメント参照) */
    grpc_op close_op;
    memset(&close_op, 0, sizeof(close_op));
    close_op.op = GRPC_OP_RECV_CLOSE_ON_SERVER;
    close_op.data.recv_close_on_server.cancelled = &cs->cancelled;
    grpc_call_start_batch(cs->call, &close_op, 1, &cs->tag_close, NULL);

    const char *message = NULL;
    int message_owned = 0;
    grpc_status_code status = process_call(cs, &message, &message_owned);
    log_rpc(cs->method, &cs->start_time, status);

    grpc_op ops[3];
    size_t nops = 0;
    memset(ops, 0, sizeof(ops));

    ops[nops].op = GRPC_OP_SEND_INITIAL_METADATA;
    nops++;

    if (status == GRPC_STATUS_OK && cs->response_payload != NULL) {
        ops[nops].op = GRPC_OP_SEND_MESSAGE;
        ops[nops].data.send_message.send_message = cs->response_payload;
        nops++;
    }

    grpc_slice status_details = grpc_slice_from_copied_string(message != NULL ? message : "");
    ops[nops].op = GRPC_OP_SEND_STATUS_FROM_SERVER;
    ops[nops].data.send_status_from_server.status = status;
    ops[nops].data.send_status_from_server.status_details = &status_details;
    nops++;

    grpc_call_error err = grpc_call_start_batch(cs->call, ops, nops, &cs->tag_finish, NULL);
    grpc_slice_unref(status_details);
    if (message_owned) free((void *)message);
    if (err != GRPC_CALL_OK) {
        fprintf(stderr, "grpc: finish batch failed: %d\n", (int)err);
    }
}

static void *worker_loop(void *arg) {
    (void)arg;
    for (;;) {
        grpc_event event =
            grpc_completion_queue_next(g_cq, gpr_inf_future(GPR_CLOCK_MONOTONIC), NULL);
        if (event.type == GRPC_QUEUE_SHUTDOWN) break;
        if (event.type != GRPC_OP_COMPLETE) continue;
        if (event.tag == &g_shutdown_tag) {
            /*
             * 【同じcompletion queueを複数スレッドで読む際の設計】grpc_server_shutdown_and_notify
             * が投げるこのtagは、g_cqを読んでいる複数のworkerスレッドのうち「どれか1つ」にだけ
             * 届く(grpc-coreは1イベントを1消費者にしか配らない)。届いたスレッドが代表して
             * grpc_server_cancel_all_calls+grpc_server_destroy+grpc_completion_queue_shutdownを
             * 行う。これによりcq待機中の他のworkerスレッドにも(cqが完全にドレインされ次第)
             * GRPC_QUEUE_SHUTDOWNが配られ、それぞれ自分でループを抜けられる
             * (grpc_server_module_stop側からcqを直接読まないのは、同じcqを「stop関数を呼ぶ
             * スレッド」と「workerスレッド」の両方が読むと、どちらがこのtagを受け取るか
             * 競合し、受け取れなかった側が永遠に待ち続けるため)。
             *
             * 【実際に踏んだバグ】以前はgrpc_server_cancel_all_calls()をgrpc_server_module_stop
             * 側(mainスレッド)からshutdown_and_notify直後に呼んでいたが、shutdown_and_notifyの
             * 通知tagは(実行中の呼び出しが無ければ)ほぼ即座にcqへ積まれるため、workerスレッドが
             * 先にこのtagを拾ってgrpc_server_destroy()してしまい、その直後にmainスレッドが
             * 「既に破棄されたserver」に対してcancel_all_calls()を呼んでSEGVした
             * (grpc_server_cancel_all_callsは「shutdown後にのみ呼べる」とgrpc.hに明記されて
             * いる一方、destroyとの前後関係については呼び出し側で保証する必要がある)。
             * 修正後: cancel_all_calls→destroyを両方ともこのtagを受け取った1スレッドの中で
             * 順番に呼ぶことで、他スレッドとの競合が起きないようにしている */
            grpc_server_cancel_all_calls(g_server);
            grpc_server_destroy(g_server);
            grpc_completion_queue_shutdown(g_cq);
            continue;
        }

        OpTag *tag = (OpTag *)event.tag;
        CallState *cs = tag->cs;
        switch (tag->phase) {
            case PHASE_NEW_CALL:
                if (event.success) {
                    handle_new_call(cs);
                } else {
                    /* サーバーshutdown中などで新規呼び出しの受付自体が失敗した場合。
                     * このCallStateは着信を受け取れなかったので即座に破棄してよい */
                    call_state_destroy(cs);
                }
                break;
            case PHASE_FINISH:
            case PHASE_CLOSE:
                cs->done_count++;
                if (cs->done_count >= 2) call_state_destroy(cs);
                break;
        }
    }
    return NULL;
}

int grpc_server_module_start(const char *addr) {
    grpc_init();

    g_server = grpc_server_create(NULL, NULL);
    if (g_server == NULL) return -1;

    for (int i = 0; i < RPC_METHOD_COUNT; i++) {
        g_method_handles[i] = grpc_server_register_method(
            g_server, kMethodPaths[i], NULL, GRPC_SRM_PAYLOAD_READ_INITIAL_BYTE_BUFFER, 0);
        if (g_method_handles[i] == NULL) {
            fprintf(stderr, "grpc: grpc_server_register_method failed for %s\n", kMethodPaths[i]);
            return -1;
        }
    }

    g_cq = grpc_completion_queue_create_for_next(NULL);
    grpc_server_register_completion_queue(g_server, g_cq, NULL);

    grpc_server_credentials *creds = grpc_insecure_server_credentials_create();
    int bound_port = grpc_server_add_http2_port(g_server, addr, creds);
    grpc_server_credentials_release(creds);
    if (bound_port == 0) {
        fprintf(stderr, "grpc: grpc_server_add_http2_port failed (addr=%s in use?)\n", addr);
        return -1;
    }

    grpc_server_start(g_server);

    for (int i = 0; i < RPC_METHOD_COUNT; i++) request_new_call((RpcMethod)i);

    for (int i = 0; i < GRPC_SERVER_WORKER_THREADS; i++) {
        if (pthread_create(&g_workers[i], NULL, worker_loop, NULL) != 0) {
            fprintf(stderr, "grpc: pthread_create failed\n");
            return -1;
        }
        g_worker_count++;
    }

    return 0;
}

void grpc_server_module_stop(void) {
    if (g_server == NULL) return;

    /* shutdown_and_notify完了後にdestroyするのがgRPC Coreの規約(grpc.hのコメント参照)。
     * 実際のgrpc_server_cancel_all_calls/grpc_server_destroy/grpc_completion_queue_shutdownは
     * すべてworker_loop側(このtagを受け取った1つのworkerスレッド)が順番に行う
     * (このスレッドから直接cancel_all_callsを呼ばない理由は上のworker_loopのコメント参照。
     * 2つのスレッドが同じg_serverに対してcancel_all_calls/destroyを競合して呼ぶと
     * SEGVする) */
    grpc_server_shutdown_and_notify(g_server, g_cq, &g_shutdown_tag);

    /* pthread_joinはworkerスレッドがループを抜けた後にしか戻らないため、
     * ここでjoinが完了した時点でg_server=NULLの代入は他スレッドとのデータ競合にならない
     * (join自体がhappens-before関係を作る) */
    for (int i = 0; i < g_worker_count; i++) pthread_join(g_workers[i], NULL);
    g_worker_count = 0;
    g_server = NULL;

    grpc_completion_queue_destroy(g_cq);
    g_cq = NULL;

    grpc_shutdown();
}
