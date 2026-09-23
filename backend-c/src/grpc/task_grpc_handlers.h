#ifndef GRPC_TASK_GRPC_HANDLERS_H
#define GRPC_TASK_GRPC_HANDLERS_H

#include "common/error.h"
#include "task/v1/task.pb-c.h"

/*
 * REST(src/http/handler.c)と同じRepository・ドメインモデル・バリデーションロジックを
 * 共有する5つのRPCハンドラ(gRPC v2、README.md「gRPC」節参照)。認証・トランザクション保護
 * ロジックを複製しない設計はbackend-cppのTaskGrpcServiceImplと同じ
 *
 * out_message: TASK_ERR_VALIDATION_ERRORのときのみ設定される(malloc済み、呼び出し側がfree()
 * すること)。それ以外の引数は関数がTASK_OKを返した場合のみ有効な値を指す
 */

/* out_response: TASK_OK時のみ設定される。呼び出し側がtask__v1__list_tasks_response__free_unpacked
 * (response, NULL)で解放すること(task_mapper.hに書いた通り、free_unpackedはunpack結果でなく
 * 手動構築した木にもそのまま使える。n_tasks/tasksが正しく揃っていれば再帰的に全て解放される) */
TaskError grpc_handle_list_tasks(int64_t user_id, const Task__V1__ListTasksRequest *req,
                                  Task__V1__ListTasksResponse **out_response);

/* out_task: TASK_OK時のみ設定される。呼び出し側がtask__v1__task__free_unpacked(t, NULL)すること */
TaskError grpc_handle_get_task(int64_t user_id, const Task__V1__GetTaskRequest *req,
                                Task__V1__Task **out_task);

TaskError grpc_handle_create_task(int64_t user_id, const Task__V1__CreateTaskRequest *req,
                                   Task__V1__Task **out_task, char **out_message);

TaskError grpc_handle_update_task(int64_t user_id, const Task__V1__UpdateTaskRequest *req,
                                   Task__V1__Task **out_task, char **out_message);

TaskError grpc_handle_delete_task(int64_t user_id, const Task__V1__DeleteTaskRequest *req);

#endif /* GRPC_TASK_GRPC_HANDLERS_H */
