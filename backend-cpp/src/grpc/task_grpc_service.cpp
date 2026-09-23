#include "grpc/task_grpc_service.hpp"

#include <chrono>
#include <iostream>
#include <optional>
#include <string>

#include "application/user_resolver.hpp"
#include "common/time.hpp"
#include "grpc/task_mapper.hpp"

namespace backend_cpp::grpcservice {

using backend_cpp::application::ResolveUserIdFromAuthHeader;
using backend_cpp::common::AppError;
using backend_cpp::common::AppErrorKind;
using backend_cpp::domain::StatusFromString;
using backend_cpp::domain::StatusToString;
using backend_cpp::domain::TaskInput;

namespace {

grpc::StatusCode ToGrpcCode(AppErrorKind kind) {
  switch (kind) {
    case AppErrorKind::kNotFound:
      return grpc::StatusCode::NOT_FOUND;
    case AppErrorKind::kUnauthorized:
      return grpc::StatusCode::UNAUTHENTICATED;
    case AppErrorKind::kDbError:
      return grpc::StatusCode::INTERNAL;
    case AppErrorKind::kInvalidRequest:
    case AppErrorKind::kInvalidId:
    case AppErrorKind::kInvalidStatus:
    case AppErrorKind::kInvalidFinishedOn:
    case AppErrorKind::kValidationError:
    default:
      return grpc::StatusCode::INVALID_ARGUMENT;
  }
}

grpc::Status ToGrpcStatus(const AppError& err) {
  std::string msg = err.message.empty() ? "invalid request" : err.message;
  if (err.kind == AppErrorKind::kNotFound) msg = "task not found";
  if (err.kind == AppErrorKind::kUnauthorized) msg = "unauthorized";
  if (err.kind == AppErrorKind::kDbError) msg = "internal error";
  return grpc::Status(ToGrpcCode(err.kind), msg);
}

// method/実際のgRPCステータス(status.error_code())/durationを1rpc1行のログとして出す
// (backend-rustのlogged()・他言語のリクエスト単位ログと同じ設計、README.md参照)
void LogRpc(const char* method, std::chrono::steady_clock::time_point start,
            const grpc::Status& status) {
  auto duration_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
                          std::chrono::steady_clock::now() - start)
                          .count();
  std::cout << "grpc method=" << method << " status=" << static_cast<int>(status.error_code())
            << " duration_ms=" << duration_ms << std::endl;
}

std::optional<std::string> AuthHeader(grpc::CallbackServerContext* context) {
  auto range = context->client_metadata().equal_range("authorization");
  if (range.first == range.second) return std::nullopt;
  return std::string(range.first->second.data(), range.first->second.size());
}

// backend(Go)のservice.validateTaskInput・task_handler.cppのValidateTaskInputと同じルール
common::Result<domain::TaskStatus> ValidateInput(const TaskInput& input) {
  if (common::Utf8CodepointLength(input.name) > 20) {
    return std::unexpected(common::ValidationError("nameは20文字以内である必要があります"));
  }
  if (input.finished_on < common::TodayUtcIso()) {
    return std::unexpected(common::ValidationError("finished_onに過去日は指定できません"));
  }
  auto status = StatusFromString(input.status_raw);
  if (!status.has_value()) {
    return std::unexpected(common::ValidationError("不明なstatus: \"" + input.status_raw + "\""));
  }
  return *status;
}

}  // namespace

grpc::ServerUnaryReactor* TaskGrpcServiceImpl::ListTasks(grpc::CallbackServerContext* context,
                                                          const task::v1::ListTasksRequest* request,
                                                          task::v1::ListTasksResponse* response) {
  auto* reactor = context->DefaultReactor();
  auto start = std::chrono::steady_clock::now();

  auto auth_header = AuthHeader(context);
  grpc::Status status = grpc::Status::OK;
  if (!auth_header) {
    status = grpc::Status(grpc::StatusCode::UNAUTHENTICATED, "unauthorized");
  } else {
    auto user_id_result = ResolveUserIdFromAuthHeader(*auth_header, repo_, dispatcher_);
    if (!user_id_result) {
      status = ToGrpcStatus(user_id_result.error());
    } else {
      // 【このフェーズの既知の制約】name/status/label_idsによる絞り込みは未対応
      // (REST v1側も同じ簡略化、README.md参照)。idのみのkeyset cursorで一覧する
      int limit = request->limit() <= 0 ? 20 : request->limit();
      std::optional<int64_t> after_id =
          request->cursor() == 0 ? std::nullopt
                                  : std::optional<int64_t>(static_cast<int64_t>(request->cursor()));
      std::optional<int64_t> next_cursor;
      auto tasks = repo_.ListCursorExternal(*user_id_result, after_id, limit, next_cursor);
      if (!tasks) {
        status = ToGrpcStatus(tasks.error());
      } else {
        for (const auto& t : *tasks) *response->add_tasks() = ToProto(t);
        response->set_next_cursor(next_cursor.has_value() ? static_cast<uint64_t>(*next_cursor) : 0);
      }
    }
  }
  LogRpc("list_tasks", start, status);
  reactor->Finish(status);
  return reactor;
}

grpc::ServerUnaryReactor* TaskGrpcServiceImpl::GetTask(grpc::CallbackServerContext* context,
                                                        const task::v1::GetTaskRequest* request,
                                                        task::v1::Task* response) {
  auto* reactor = context->DefaultReactor();
  auto start = std::chrono::steady_clock::now();

  auto auth_header = AuthHeader(context);
  grpc::Status status = grpc::Status::OK;
  if (!auth_header) {
    status = grpc::Status(grpc::StatusCode::UNAUTHENTICATED, "unauthorized");
  } else {
    auto user_id_result = ResolveUserIdFromAuthHeader(*auth_header, repo_, dispatcher_);
    if (!user_id_result) {
      status = ToGrpcStatus(user_id_result.error());
    } else {
      auto task = repo_.FindById(static_cast<int64_t>(request->id()), *user_id_result);
      if (!task) {
        status = ToGrpcStatus(task.error());
      } else {
        *response = ToProto(*task);
      }
    }
  }
  LogRpc("get_task", start, status);
  reactor->Finish(status);
  return reactor;
}

grpc::ServerUnaryReactor* TaskGrpcServiceImpl::CreateTask(grpc::CallbackServerContext* context,
                                                           const task::v1::CreateTaskRequest* request,
                                                           task::v1::Task* response) {
  auto* reactor = context->DefaultReactor();
  auto start = std::chrono::steady_clock::now();

  auto auth_header = AuthHeader(context);
  grpc::Status status = grpc::Status::OK;
  if (!auth_header) {
    status = grpc::Status(grpc::StatusCode::UNAUTHENTICATED, "unauthorized");
  } else {
    auto user_id_result = ResolveUserIdFromAuthHeader(*auth_header, repo_, dispatcher_);
    if (!user_id_result) {
      status = ToGrpcStatus(user_id_result.error());
    } else if (!common::IsValidIsoDate(request->finished_on())) {
      status = ToGrpcStatus(common::InvalidFinishedOn());
    } else {
      TaskInput input;
      input.name = request->name();
      if (request->has_description()) input.description = request->description();
      input.status_raw = request->status();
      input.finished_on = request->finished_on();
      for (auto id : request->label_ids()) input.label_ids.push_back(static_cast<int64_t>(id));

      auto validated = ValidateInput(input);
      if (!validated) {
        status = ToGrpcStatus(validated.error());
      } else {
        auto created = repo_.Create(*user_id_result, input, *validated);
        if (!created) {
          status = ToGrpcStatus(created.error());
        } else {
          auto task = repo_.FindById(*created, *user_id_result);
          if (!task) {
            status = ToGrpcStatus(task.error());
          } else {
            *response = ToProto(*task);
          }
        }
      }
    }
  }
  LogRpc("create_task", start, status);
  reactor->Finish(status);
  return reactor;
}

grpc::ServerUnaryReactor* TaskGrpcServiceImpl::UpdateTask(grpc::CallbackServerContext* context,
                                                           const task::v1::UpdateTaskRequest* request,
                                                           task::v1::Task* response) {
  auto* reactor = context->DefaultReactor();
  auto start = std::chrono::steady_clock::now();

  auto auth_header = AuthHeader(context);
  grpc::Status status = grpc::Status::OK;
  if (!auth_header) {
    status = grpc::Status(grpc::StatusCode::UNAUTHENTICATED, "unauthorized");
  } else {
    auto user_id_result = ResolveUserIdFromAuthHeader(*auth_header, repo_, dispatcher_);
    if (!user_id_result) {
      status = ToGrpcStatus(user_id_result.error());
    } else if (!common::IsValidIsoDate(request->finished_on())) {
      status = ToGrpcStatus(common::InvalidFinishedOn());
    } else {
      TaskInput input;
      input.name = request->name();
      if (request->has_description()) input.description = request->description();
      input.status_raw = request->status();
      input.finished_on = request->finished_on();
      for (auto id : request->label_ids()) input.label_ids.push_back(static_cast<int64_t>(id));

      auto validated = ValidateInput(input);
      if (!validated) {
        status = ToGrpcStatus(validated.error());
      } else {
        const int64_t id = static_cast<int64_t>(request->id());
        auto updated = repo_.Update(id, *user_id_result, input, *validated);
        if (!updated) {
          status = ToGrpcStatus(updated.error());
        } else if (!*updated) {
          status = ToGrpcStatus(common::NotFound());
        } else {
          auto task = repo_.FindById(id, *user_id_result);
          if (!task) {
            status = ToGrpcStatus(task.error());
          } else {
            *response = ToProto(*task);
          }
        }
      }
    }
  }
  LogRpc("update_task", start, status);
  reactor->Finish(status);
  return reactor;
}

grpc::ServerUnaryReactor* TaskGrpcServiceImpl::DeleteTask(grpc::CallbackServerContext* context,
                                                           const task::v1::DeleteTaskRequest* request,
                                                           task::v1::DeleteTaskResponse* /*response*/) {
  auto* reactor = context->DefaultReactor();
  auto start = std::chrono::steady_clock::now();

  auto auth_header = AuthHeader(context);
  grpc::Status status = grpc::Status::OK;
  if (!auth_header) {
    status = grpc::Status(grpc::StatusCode::UNAUTHENTICATED, "unauthorized");
  } else {
    auto user_id_result = ResolveUserIdFromAuthHeader(*auth_header, repo_, dispatcher_);
    if (!user_id_result) {
      status = ToGrpcStatus(user_id_result.error());
    } else {
      // tasksとtask_labelsの削除は1つのトランザクションで包まれている
      // (Repository::Delete、README.md「backend多言語比較」節参照)
      auto deleted = repo_.Delete(static_cast<int64_t>(request->id()), *user_id_result);
      if (!deleted) {
        status = ToGrpcStatus(deleted.error());
      } else if (!*deleted) {
        status = ToGrpcStatus(common::NotFound());
      }
    }
  }
  LogRpc("delete_task", start, status);
  reactor->Finish(status);
  return reactor;
}

}  // namespace backend_cpp::grpcservice
