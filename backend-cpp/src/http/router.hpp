#pragma once

#include <boost/asio/awaitable.hpp>
#include <functional>
#include <optional>
#include <string>
#include <vector>

#include "http/http_types.hpp"

namespace backend_cpp::http {

namespace asio = boost::asio;

// このプロジェクトのルーティングは/internal/v1/tasksと/internal/v1/tasks/{id}の
// 2パターンのみなので、汎用的なルーティングライブラリは使わず自作する(ORM同様、
// フレームワークに頼らず「何が起きているか」を見える化する方針、README.md参照)
using Handler = std::function<asio::awaitable<HttpResponse>(const HttpRequest&, int64_t id)>;

struct Route {
  std::string method;
  std::string prefix;  // 例: "/internal/v1/tasks"
  bool needs_id;        // 例: "/internal/v1/tasks/{id}"
  Handler handler;
};

class Router {
 public:
  void Add(std::string method, std::string prefix, bool needs_id, Handler handler);
  asio::awaitable<HttpResponse> Dispatch(const HttpRequest& req);

 private:
  std::vector<Route> routes_;
};

}  // namespace backend_cpp::http
