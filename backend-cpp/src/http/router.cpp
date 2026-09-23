#include "http/router.hpp"

#include <charconv>
#include <chrono>

#include "common/logging.hpp"

namespace backend_cpp::http {

Router::Router(std::string log_module) : log_module_(std::move(log_module)) {}

void Router::Add(std::string method, std::string prefix, bool needs_id, Handler handler) {
  routes_.push_back(Route{std::move(method), std::move(prefix), needs_id, std::move(handler)});
}

namespace {
// targetから "パス部分" だけを取り出す(クエリ文字列を切り離す)
std::string PathOnly(const std::string& target) {
  auto pos = target.find('?');
  return pos == std::string::npos ? target : target.substr(0, pos);
}

// リクエスト単位のログ(INFO、既定で常に出る)。gRPC側のLogRpc(src/grpc/task_grpc_service.cpp、
// 既存)と同じkey=value形式・同じsteady_clockによる計測方法にそろえている。ログに使うステータスは
// ハンドラ(型付き戻り値HttpResponse::status)が実際に返した値そのもの(推測・再計算しない)。
// log_moduleにより"rest method=..."/"external method=..."を1つの実装で出し分ける
// (README.md「ログについて」参照)
void LogRequest(const std::string& log_module, const std::string& method, const std::string& path,
                int status, std::chrono::steady_clock::time_point start) {
  auto duration_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
                          std::chrono::steady_clock::now() - start)
                          .count();
  common::LogInfo(log_module + " method=" + method + " path=" + path +
                   " status=" + std::to_string(status) +
                   " duration_ms=" + std::to_string(duration_ms));
}
}  // namespace

asio::awaitable<HttpResponse> Router::Dispatch(const HttpRequest& req) {
  const std::string path = PathOnly(req.target);
  const auto start = std::chrono::steady_clock::now();

  for (const auto& route : routes_) {
    if (route.method != req.method) continue;

    if (!route.needs_id) {
      if (path == route.prefix) {
        HttpResponse resp = co_await route.handler(req, 0);
        LogRequest(log_module_, req.method, path, resp.status, start);
        co_return resp;
      }
      continue;
    }

    // "{prefix}/{id}" 形式(idは数字のみ)
    if (path.size() > route.prefix.size() + 1 && path.starts_with(route.prefix) &&
        path[route.prefix.size()] == '/') {
      const std::string id_str = path.substr(route.prefix.size() + 1);
      int64_t id = 0;
      auto [ptr, ec] = std::from_chars(id_str.data(), id_str.data() + id_str.size(), id);
      if (ec == std::errc() && ptr == id_str.data() + id_str.size()) {
        HttpResponse resp = co_await route.handler(req, id);
        LogRequest(log_module_, req.method, path, resp.status, start);
        co_return resp;
      }
      // idが数値でない場合、backend(Go)等と同じくinvalid_idとして扱う
      HttpResponse resp = HttpResponse::Json(400, R"({"error":"invalid_id"})");
      LogRequest(log_module_, req.method, path, resp.status, start);
      co_return resp;
    }
  }
  HttpResponse resp = HttpResponse::Json(404, R"({"error":"not_found"})");
  LogRequest(log_module_, req.method, path, resp.status, start);
  co_return resp;
}

}  // namespace backend_cpp::http
