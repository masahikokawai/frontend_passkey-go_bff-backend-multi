#pragma once

#include "auth/jwt.hpp"
#include "repository/task_repository.hpp"
#include "task.grpc.pb.h"

namespace backend_cpp::grpcservice {

// gRPC v2(:9099、CONTRACT.mdセクション5)。REST v1(task_handler)と同じRepository・
// ドメインモデル・認証Dispatcherを共有する(認証・バリデーション・トランザクション保護の
// ロジックを複製しない設計、README.md「gRPC」節参照)
//
// 【スレッドモデルについての設計判断】RESTはBoost.Asioのio_context(非同期)上で動くため、
// ブロッキングなDB呼び出しをdb_thread_poolへ明示的にディスパッチする必要があった
// (db/blocking.hpp参照)。gRPC C++のCallback APIは、各RPCをgRPC自身が管理する
// スレッドプール上で呼び出すため、Boost.Asioのio_contextとは完全に別の実行環境になる。
// このスレッドプール自体がブロッキング処理を想定した設計のため、RepositoryへのDB呼び出しは
// ここでは(REST側のようにRunBlockingへ包まず)そのまま直接ブロッキング呼び出しでよい
// (gRPC公式ドキュメントでも、Callback APIのハンドラ内でブロッキング処理を行うこと自体は
// 許容されている。極端に高い同時実行数が必要な場合のみ、専用スレッドプールへさらに
// ディスパッチすることが推奨される)
class TaskGrpcServiceImpl final : public task::v1::TaskService::CallbackService {
 public:
  TaskGrpcServiceImpl(repository::TaskRepository& repo, auth::Dispatcher& dispatcher)
      : repo_(repo), dispatcher_(dispatcher) {}

  grpc::ServerUnaryReactor* ListTasks(grpc::CallbackServerContext* context,
                                       const task::v1::ListTasksRequest* request,
                                       task::v1::ListTasksResponse* response) override;
  grpc::ServerUnaryReactor* GetTask(grpc::CallbackServerContext* context,
                                     const task::v1::GetTaskRequest* request,
                                     task::v1::Task* response) override;
  grpc::ServerUnaryReactor* CreateTask(grpc::CallbackServerContext* context,
                                        const task::v1::CreateTaskRequest* request,
                                        task::v1::Task* response) override;
  grpc::ServerUnaryReactor* UpdateTask(grpc::CallbackServerContext* context,
                                        const task::v1::UpdateTaskRequest* request,
                                        task::v1::Task* response) override;
  grpc::ServerUnaryReactor* DeleteTask(grpc::CallbackServerContext* context,
                                        const task::v1::DeleteTaskRequest* request,
                                        task::v1::DeleteTaskResponse* response) override;

 private:
  repository::TaskRepository& repo_;
  auth::Dispatcher& dispatcher_;
};

}  // namespace backend_cpp::grpcservice
