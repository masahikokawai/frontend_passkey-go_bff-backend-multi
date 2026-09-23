#include "external/external_handler.hpp"

#include <nlohmann/json.hpp>

#include <sstream>

#include "db/blocking.hpp"

namespace backend_cpp::external {

using json = nlohmann::json;
using backend_cpp::db::RunBlocking;
using backend_cpp::domain::Task;

namespace {

// backend(Go)のtaskDTOToJSON(internal/handler/external/task.go)と1文字も変えていない形状
// 内部CRUDのレスポンスとは異なりuser_idを含めない
json TaskToJson(const Task& t) {
  json labels = json::array();
  for (const auto& l : t.labels) labels.push_back({{"id", l.id}, {"name", l.name}});
  auto iso_datetime = [](const std::string& mysql_dt) {
    std::string s = mysql_dt;
    if (s.size() > 10) s[10] = 'T';
    return s + "+00:00";
  };
  return json{
      {"id", t.id},
      {"name", t.name},
      {"description", t.description.has_value() ? json(*t.description) : json(nullptr)},
      {"status", domain::StatusToString(t.status)},
      {"finished_on", t.finished_on},
      {"labels", labels},
      {"created_at", iso_datetime(t.created_at)},
      {"updated_at", iso_datetime(t.updated_at)},
  };
}

std::map<std::string, std::string> ParseQuery(const std::string& target) {
  std::map<std::string, std::string> out;
  auto qpos = target.find('?');
  if (qpos == std::string::npos) return out;
  std::istringstream ss(target.substr(qpos + 1));
  std::string pair;
  while (std::getline(ss, pair, '&')) {
    auto eq = pair.find('=');
    if (eq == std::string::npos) continue;
    out[pair.substr(0, eq)] = pair.substr(eq + 1);
  }
  return out;
}

}  // namespace

asio::awaitable<HttpResponse> ExternalHandler::List(const HttpRequest& req, int64_t) {
  // Client Credentials Grant(Keycloak発行)のみ受け付ける。ローカルHMAC/RSAは対象外
  // (backend(Go)のRequireExternalClientAuthと同じ2段チェック: JWKS検証+azp一致)
  auto auth_it = req.headers.find("authorization");
  if (auth_it == req.headers.end()) {
    co_return HttpResponse::Json(401, R"({"error":"unauthenticated"})");
  }
  auto claims = dispatcher_.Verify(auth_it->second);
  if (!claims || auth::IsLocalIssuer(claims->iss) || claims->azp != external_api_client_id_) {
    co_return HttpResponse::Json(401, R"({"error":"unauthenticated"})");
  }

  auto q = ParseQuery(req.target);
  auto user_id_it = q.find("user_id");
  if (user_id_it == q.end() || user_id_it->second.empty()) {
    co_return HttpResponse::Json(400, R"({"error":"user_id_required"})");
  }
  int64_t user_id = 0;
  try {
    user_id = std::stoll(user_id_it->second);
  } catch (...) {
    co_return HttpResponse::Json(400, R"({"error":"invalid_user_id"})");
  }

  // backend.external-tasks-pagination-v2(5言語で共有する1つのFeature Flag)で
  // offset(v1)/cursor(v2)を切り替える(backend-rustと同じ設計)
  bool use_v2 = flags_.Variation("backend.external-tasks-pagination-v2", "off") == "on";

  if (use_v2) {
    std::optional<int64_t> after_id;
    if (auto it = q.find("cursor"); it != q.end() && !it->second.empty()) {
      try {
        after_id = std::stoll(it->second);
      } catch (...) {
      }
    }
    int limit = 10;
    if (auto it = q.find("limit"); it != q.end()) {
      try {
        limit = std::max(1, std::stoi(it->second));
      } catch (...) {
      }
    }
    std::optional<int64_t> next_cursor;
    auto result = co_await RunBlocking(db_pool_, [this, user_id, after_id, limit, &next_cursor] {
      return repo_.ListCursorExternal(user_id, after_id, limit, next_cursor);
    });
    if (!result) co_return HttpResponse::Json(500, R"({"error":"internal_server_error"})");
    json tasks = json::array();
    for (const auto& t : *result) tasks.push_back(TaskToJson(t));
    json body{{"tasks", tasks}};
    body["next_cursor"] = next_cursor.has_value() ? json(std::to_string(*next_cursor)) : json(nullptr);
    co_return HttpResponse::Json(200, body.dump());
  }

  int page = 1;
  int page_size = 10;
  if (auto it = q.find("page"); it != q.end()) {
    try {
      page = std::max(1, std::stoi(it->second));
    } catch (...) {
    }
  }
  if (auto it = q.find("page_size"); it != q.end()) {
    try {
      page_size = std::max(1, std::stoi(it->second));
    } catch (...) {
    }
  }
  int64_t total = 0;
  auto result = co_await RunBlocking(db_pool_, [this, user_id, page, page_size, &total] {
    return repo_.ListOffsetExternal(user_id, page, page_size, total);
  });
  if (!result) co_return HttpResponse::Json(500, R"({"error":"internal_server_error"})");
  json tasks = json::array();
  for (const auto& t : *result) tasks.push_back(TaskToJson(t));
  co_return HttpResponse::Json(
      200, json{{"tasks", tasks}, {"page", page}, {"page_size", page_size}, {"total", total}}.dump());
}

}  // namespace backend_cpp::external
