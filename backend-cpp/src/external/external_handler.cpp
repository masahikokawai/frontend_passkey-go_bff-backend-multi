#include "external/external_handler.hpp"

#include <nlohmann/json.hpp>

#include <sstream>

#include "common/logging.hpp"
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

}  // namespace

std::map<std::string, std::string> ParseQueryParams(const std::string& target) {
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

UserIdParseResult ParseUserId(const std::map<std::string, std::string>& q, int64_t& user_id) {
  auto it = q.find("user_id");
  if (it == q.end() || it->second.empty()) return UserIdParseResult::kRequired;
  try {
    user_id = std::stoll(it->second);
  } catch (...) {
    return UserIdParseResult::kInvalid;
  }
  return UserIdParseResult::kOk;
}

void ParseOffsetPaging(const std::map<std::string, std::string>& q, int& page, int& page_size) {
  page = 1;
  page_size = 10;
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
}

void ParseCursorPaging(const std::map<std::string, std::string>& q, std::optional<int64_t>& after_id,
                       int& limit) {
  after_id = std::nullopt;
  limit = 10;
  if (auto it = q.find("cursor"); it != q.end() && !it->second.empty()) {
    try {
      after_id = std::stoll(it->second);
    } catch (...) {
    }
  }
  if (auto it = q.find("limit"); it != q.end()) {
    try {
      limit = std::max(1, std::stoi(it->second));
    } catch (...) {
    }
  }
}

bool UseCursorPaging(const std::string& variation) { return variation == "on"; }

std::optional<auth::Claims> RequireExternalClientAuth(
    auth::Dispatcher& dispatcher, const std::optional<std::string>& authorization_header,
    const std::string& external_api_client_id) {
  if (!authorization_header.has_value()) return std::nullopt;
  auto claims = dispatcher.Verify(*authorization_header);
  if (!claims || auth::IsLocalIssuer(claims->iss) || claims->azp != external_api_client_id) {
    return std::nullopt;
  }
  return claims;
}

asio::awaitable<HttpResponse> ExternalHandler::List(const HttpRequest& req, int64_t) {
  // Client Credentials Grant(Keycloak発行)のみ受け付ける。ローカルHMAC/RSAは対象外
  // (backend(Go)のRequireExternalClientAuthと同じ2段チェック: JWKS検証+azp一致)
  std::optional<std::string> auth_header;
  if (auto it = req.headers.find("authorization"); it != req.headers.end()) auth_header = it->second;
  auto claims = RequireExternalClientAuth(dispatcher_, auth_header, external_api_client_id_);
  if (!claims) {
    co_return HttpResponse::Json(401, R"({"error":"unauthenticated"})");
  }

  auto q = ParseQueryParams(req.target);
  int64_t user_id = 0;
  switch (ParseUserId(q, user_id)) {
    case UserIdParseResult::kRequired:
      co_return HttpResponse::Json(400, R"({"error":"user_id_required"})");
    case UserIdParseResult::kInvalid:
      co_return HttpResponse::Json(400, R"({"error":"invalid_user_id"})");
    case UserIdParseResult::kOk:
      break;
  }

  // backend.external-tasks-pagination-v2(5言語で共有する1つのFeature Flag)で
  // offset(v1)/cursor(v2)を切り替える(backend-rustと同じ設計)
  bool use_v2 = UseCursorPaging(flags_.Variation("backend.external-tasks-pagination-v2", "off"));

  if (use_v2) {
    std::optional<int64_t> after_id;
    int limit = 10;
    ParseCursorPaging(q, after_id, limit);
    common::LogDebug("external debug: list_cursor user_id=" + std::to_string(user_id) +
                      " after_id=" + std::to_string(after_id.value_or(0)) +
                      " limit=" + std::to_string(limit));
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
  ParseOffsetPaging(q, page, page_size);
  common::LogDebug("external debug: list_offset user_id=" + std::to_string(user_id) +
                    " page=" + std::to_string(page) + " page_size=" + std::to_string(page_size));
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
