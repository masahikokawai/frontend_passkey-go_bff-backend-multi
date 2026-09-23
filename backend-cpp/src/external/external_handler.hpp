#pragma once

#include <boost/asio/awaitable.hpp>
#include <boost/asio/thread_pool.hpp>

#include "auth/jwt.hpp"
#include "flags/feature_flag_poller.hpp"
#include "http/http_types.hpp"
#include "repository/task_repository.hpp"

namespace backend_cpp::external {

namespace asio = boost::asio;
using backend_cpp::http::HttpRequest;
using backend_cpp::http::HttpResponse;

// CONTRACT.mdセクション11: bffを経由しない外部公開API
// Client Credentials Grant(Keycloak発行、azp=EXTERNAL_API_CLIENT_IDのみ受け付ける)
class ExternalHandler {
 public:
  ExternalHandler(repository::TaskRepository& repo, asio::thread_pool& db_pool,
                   auth::Dispatcher& dispatcher, flags::FeatureFlagPoller& flags,
                   std::string external_api_client_id)
      : repo_(repo),
        db_pool_(db_pool),
        dispatcher_(dispatcher),
        flags_(flags),
        external_api_client_id_(std::move(external_api_client_id)) {}

  asio::awaitable<HttpResponse> List(const HttpRequest& req, int64_t /*unused*/);

 private:
  repository::TaskRepository& repo_;
  asio::thread_pool& db_pool_;
  auth::Dispatcher& dispatcher_;
  flags::FeatureFlagPoller& flags_;
  std::string external_api_client_id_;
};

}  // namespace backend_cpp::external
