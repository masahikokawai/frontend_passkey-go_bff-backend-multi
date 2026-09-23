#pragma once

#include <boost/asio/awaitable.hpp>
#include <boost/asio/thread_pool.hpp>

#include "auth/jwt.hpp"
#include "http/http_types.hpp"
#include "repository/task_repository.hpp"

namespace backend_cpp::application {

namespace asio = boost::asio;
using backend_cpp::http::HttpRequest;
using backend_cpp::http::HttpResponse;

// Authorizationヘッダを auth::Dispatcher で検証し、Keycloak/ローカルHMAC/ローカルRSAの
// 3issuerからuser_idを解決する(backend(Go)のresolve_user_id、backend-rustと同じ設計)
// Authorizationヘッダが無い場合のみ `X-Debug-User-Id` ヘッダにフォールバックする
// (docker compose無しでの単体動作確認用、README.md参照)
class TaskHandler {
 public:
  TaskHandler(repository::TaskRepository& repo, asio::thread_pool& db_pool,
              auth::Dispatcher& dispatcher)
      : repo_(repo), db_pool_(db_pool), dispatcher_(dispatcher) {}

  asio::awaitable<HttpResponse> List(const HttpRequest& req, int64_t /*unused*/);
  asio::awaitable<HttpResponse> Get(const HttpRequest& req, int64_t id);
  asio::awaitable<HttpResponse> Create(const HttpRequest& req, int64_t /*unused*/);
  asio::awaitable<HttpResponse> Update(const HttpRequest& req, int64_t id);
  asio::awaitable<HttpResponse> Delete(const HttpRequest& req, int64_t id);

 private:
  repository::TaskRepository& repo_;
  asio::thread_pool& db_pool_;
  auth::Dispatcher& dispatcher_;
};

}  // namespace backend_cpp::application
