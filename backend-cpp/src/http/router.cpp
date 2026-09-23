#include "http/router.hpp"

#include <charconv>

namespace backend_cpp::http {

void Router::Add(std::string method, std::string prefix, bool needs_id, Handler handler) {
  routes_.push_back(Route{std::move(method), std::move(prefix), needs_id, std::move(handler)});
}

namespace {
// targetから "パス部分" だけを取り出す(クエリ文字列を切り離す)
std::string PathOnly(const std::string& target) {
  auto pos = target.find('?');
  return pos == std::string::npos ? target : target.substr(0, pos);
}
}  // namespace

asio::awaitable<HttpResponse> Router::Dispatch(const HttpRequest& req) {
  const std::string path = PathOnly(req.target);

  for (const auto& route : routes_) {
    if (route.method != req.method) continue;

    if (!route.needs_id) {
      if (path == route.prefix) {
        co_return co_await route.handler(req, 0);
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
        co_return co_await route.handler(req, id);
      }
      // idが数値でない場合、backend(Go)等と同じくinvalid_idとして扱う
      co_return HttpResponse::Json(400, R"({"error":"invalid_id"})");
    }
  }
  co_return HttpResponse::Json(404, R"({"error":"not_found"})");
}

}  // namespace backend_cpp::http
