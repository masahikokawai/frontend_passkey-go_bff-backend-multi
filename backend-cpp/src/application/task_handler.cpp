#include "application/task_handler.hpp"

#include <nlohmann/json.hpp>

#include <sstream>
#include <string>

#include "application/user_resolver.hpp"
#include "common/error.hpp"
#include "common/logging.hpp"
#include "common/time.hpp"
#include "db/blocking.hpp"

namespace backend_cpp::application {

using json = nlohmann::json;
using backend_cpp::common::AppError;
using backend_cpp::common::HttpStatusFor;
using backend_cpp::common::InvalidFinishedOn;
using backend_cpp::common::InvalidId;
using backend_cpp::common::InvalidRequest;
using backend_cpp::common::JsonBodyFor;
using backend_cpp::common::ValidationError;
using backend_cpp::db::RunBlocking;
using backend_cpp::domain::StatusFromString;
using backend_cpp::domain::StatusToString;
using backend_cpp::domain::Task;
using backend_cpp::domain::TaskInput;
using backend_cpp::domain::TaskStatus;

namespace {

// CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
// 【backend(Go)の実際の挙動に合わせた既知の差異】backendはuser_idをレスポンスに含めない
// (backend-js-express/src/rest/task.js taskToJsonと1文字も変えていない)
json TaskToJson(const Task& t) {
  json labels = json::array();
  for (const auto& l : t.labels) labels.push_back({{"id", l.id}, {"name", l.name}});
  auto iso_datetime = [](const std::string& mysql_dt) {
    // "YYYY-MM-DD HH:MM:SS" -> "YYYY-MM-DDTHH:MM:SS+00:00"
    std::string s = mysql_dt;
    if (s.size() > 10) s[10] = 'T';
    return s + "+00:00";
  };
  return json{
      {"id", t.id},
      {"name", t.name},
      {"description", t.description.has_value() ? json(*t.description) : json(nullptr)},
      {"status", StatusToString(t.status)},
      {"finished_on", t.finished_on},
      {"labels", labels},
      {"created_at", iso_datetime(t.created_at)},
      {"updated_at", iso_datetime(t.updated_at)},
  };
}

// backend(Go)のresolve_user_id・backend-rustのsrc/auth/mod.rsと同じ設計:
// ローカル発行issuerはsubがそのままusers.id、Keycloak発行issuerはsub=keycloak_subを
// user_keycloaksテーブル経由で引く。Authorizationヘッダが無い場合のみ
// X-Debug-User-Idへフォールバックする(README.md参照)
asio::awaitable<common::Result<int64_t>> ResolveUserId(const HttpRequest& req,
                                                        repository::TaskRepository& repo,
                                                        asio::thread_pool& db_pool,
                                                        auth::Dispatcher& dispatcher) {
  auto auth_it = req.headers.find("authorization");
  if (auth_it == req.headers.end()) {
    auto debug_it = req.headers.find("x-debug-user-id");
    if (debug_it == req.headers.end()) co_return std::unexpected(common::Unauthorized());
    try {
      co_return std::stoll(debug_it->second);
    } catch (...) {
      co_return std::unexpected(common::Unauthorized());
    }
  }

  // 検証+user_id解決(ブロッキング)はREST/gRPC共通のapplication::ResolveUserIdFromAuthHeaderに
  // 委譲する(認証ロジックの複製を避ける、README.md「gRPC」節参照)。RESTはここでdb_thread_pool
  // へディスパッチしてio_contextのスレッドをブロックしない
  std::string auth_header = auth_it->second;
  co_return co_await RunBlocking(db_pool, [&repo, &dispatcher, auth_header] {
    return ResolveUserIdFromAuthHeader(auth_header, repo, dispatcher);
  });
}

// backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い
// (backend-js-express/src/rest/task.js toTaskInputと同じロジック)
common::Result<TaskInput> ParseTaskInput(const std::string& body) {
  json j;
  try {
    j = json::parse(body);
  } catch (...) {
    return std::unexpected(InvalidRequest());
  }
  TaskInput input;
  input.name = j.value("name", "");
  input.status_raw = j.value("status", "");
  input.finished_on = j.value("finished_on", "");
  if (input.name.empty() || input.status_raw.empty() || input.finished_on.empty()) {
    return std::unexpected(InvalidRequest());
  }
  if (!common::IsValidIsoDate(input.finished_on)) {
    return std::unexpected(InvalidFinishedOn());
  }
  if (j.contains("description") && !j["description"].is_null()) {
    input.description = j["description"].get<std::string>();
  }
  if (j.contains("label_ids") && j["label_ids"].is_array()) {
    for (const auto& v : j["label_ids"]) {
      if (v.is_number_integer()) input.label_ids.push_back(v.get<int64_t>());
    }
  }
  return input;
}

// backend(Go)のservice.validateTaskInputと同じルール(README.md・common/time.hpp参照)
common::Result<TaskStatus> ValidateTaskInput(const TaskInput& input) {
  if (common::Utf8CodepointLength(input.name) > 20) {
    return std::unexpected(ValidationError("nameは20文字以内である必要があります"));
  }
  if (input.finished_on < common::TodayUtcIso()) {
    return std::unexpected(ValidationError("finished_onに過去日は指定できません"));
  }
  auto status = StatusFromString(input.status_raw);
  if (!status.has_value()) {
    return std::unexpected(ValidationError("不明なstatus: \"" + input.status_raw + "\""));
  }
  return *status;
}

HttpResponse ErrorResponse(const AppError& err) {
  return HttpResponse::Json(HttpStatusFor(err), JsonBodyFor(err));
}

}  // namespace

asio::awaitable<HttpResponse> TaskHandler::List(const HttpRequest& req, int64_t) {
  auto user_id_result = co_await ResolveUserId(req, repo_, db_pool_, dispatcher_);
  if (!user_id_result) co_return ErrorResponse(user_id_result.error());
  const int64_t user_id = *user_id_result;
  int limit = 20;
  int offset = 0;
  // 簡易クエリパース(limit/offsetのみ、このフェーズではフィルタ未対応)
  auto qpos = req.target.find('?');
  if (qpos != std::string::npos) {
    std::string qs = req.target.substr(qpos + 1);
    std::istringstream ss(qs);
    std::string pair;
    while (std::getline(ss, pair, '&')) {
      auto eq = pair.find('=');
      if (eq == std::string::npos) continue;
      std::string key = pair.substr(0, eq);
      std::string val = pair.substr(eq + 1);
      if (key == "limit") limit = std::stoi(val);
      if (key == "offset") offset = std::stoi(val);
    }
  }
  common::LogDebug("rest debug: list user_id=" + std::to_string(user_id) +
                    " limit=" + std::to_string(limit) + " offset=" + std::to_string(offset));

  int64_t total = 0;
  auto result = co_await RunBlocking(db_pool_, [this, user_id, limit, offset, &total] {
    return repo_.ListByUser(user_id, limit, offset, total);
  });
  if (!result) co_return ErrorResponse(result.error());

  json tasks = json::array();
  for (const auto& t : *result) tasks.push_back(TaskToJson(t));
  co_return HttpResponse::Json(200, json{{"tasks", tasks}, {"total", total}, {"limit", limit},
                                          {"offset", offset}}
                                         .dump());
}

asio::awaitable<HttpResponse> TaskHandler::Get(const HttpRequest& req, int64_t id) {
  auto user_id_result = co_await ResolveUserId(req, repo_, db_pool_, dispatcher_);
  if (!user_id_result) co_return ErrorResponse(user_id_result.error());
  const int64_t user_id = *user_id_result;
  auto result = co_await RunBlocking(db_pool_, [this, id, user_id] {
    return repo_.FindById(id, user_id);
  });
  if (!result) co_return ErrorResponse(result.error());
  co_return HttpResponse::Json(200, TaskToJson(*result).dump());
}

asio::awaitable<HttpResponse> TaskHandler::Create(const HttpRequest& req, int64_t) {
  auto user_id_result = co_await ResolveUserId(req, repo_, db_pool_, dispatcher_);
  if (!user_id_result) co_return ErrorResponse(user_id_result.error());
  const int64_t user_id = *user_id_result;
  auto parsed = ParseTaskInput(req.body);
  if (!parsed) co_return ErrorResponse(parsed.error());
  auto status = ValidateTaskInput(*parsed);
  if (!status) co_return ErrorResponse(status.error());

  TaskInput input = *parsed;
  TaskStatus st = *status;
  auto created = co_await RunBlocking(db_pool_, [this, user_id, &input, st] {
    return repo_.Create(user_id, input, st);
  });
  if (!created) co_return ErrorResponse(created.error());

  auto task = co_await RunBlocking(db_pool_, [this, id = *created, user_id] {
    return repo_.FindById(id, user_id);
  });
  if (!task) co_return ErrorResponse(task.error());
  co_return HttpResponse::Json(201, TaskToJson(*task).dump());
}

asio::awaitable<HttpResponse> TaskHandler::Update(const HttpRequest& req, int64_t id) {
  auto user_id_result = co_await ResolveUserId(req, repo_, db_pool_, dispatcher_);
  if (!user_id_result) co_return ErrorResponse(user_id_result.error());
  const int64_t user_id = *user_id_result;
  auto parsed = ParseTaskInput(req.body);
  if (!parsed) co_return ErrorResponse(parsed.error());
  auto status = ValidateTaskInput(*parsed);
  if (!status) co_return ErrorResponse(status.error());

  TaskInput input = *parsed;
  TaskStatus st = *status;
  auto updated = co_await RunBlocking(db_pool_, [this, id, user_id, &input, st] {
    return repo_.Update(id, user_id, input, st);
  });
  if (!updated) co_return ErrorResponse(updated.error());
  if (!*updated) co_return ErrorResponse(common::NotFound());

  auto task = co_await RunBlocking(db_pool_, [this, id, user_id] {
    return repo_.FindById(id, user_id);
  });
  if (!task) co_return ErrorResponse(task.error());
  co_return HttpResponse::Json(200, TaskToJson(*task).dump());
}

asio::awaitable<HttpResponse> TaskHandler::Delete(const HttpRequest& req, int64_t id) {
  auto user_id_result = co_await ResolveUserId(req, repo_, db_pool_, dispatcher_);
  if (!user_id_result) co_return ErrorResponse(user_id_result.error());
  const int64_t user_id = *user_id_result;
  auto deleted = co_await RunBlocking(db_pool_, [this, id, user_id] {
    return repo_.Delete(id, user_id);
  });
  if (!deleted) co_return ErrorResponse(deleted.error());
  if (!*deleted) co_return ErrorResponse(common::NotFound());
  co_return HttpResponse::NoContent();
}

}  // namespace backend_cpp::application
