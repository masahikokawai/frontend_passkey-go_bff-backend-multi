#include <boost/asio/co_spawn.hpp>
#include <boost/asio/detached.hpp>
#include <boost/asio/io_context.hpp>
#include <boost/asio/ip/tcp.hpp>
#include <boost/asio/signal_set.hpp>
#include <boost/asio/thread_pool.hpp>
#include <grpcpp/grpcpp.h>

#include <iostream>
#include <memory>
#include <thread>
#include <vector>

#include "application/task_handler.hpp"
#include "auth/jwt.hpp"
#include "config.hpp"
#include "db/connection_pool.hpp"
#include "external/external_handler.hpp"
#include "flags/feature_flag_poller.hpp"
#include "grpc/task_grpc_service.hpp"
#include "http/router.hpp"
#include "http/session.hpp"
#include "repository/task_repository.hpp"

namespace asio = boost::asio;
using asio::ip::tcp;
using namespace backend_cpp;  // NOLINT

int main() {
  Config config = Config::FromEnv();
  std::cout << "backend-cpp starting: HTTP_ADDR=:" << config.http_addr
            << " db=" << config.db_host << ":" << config.db_port << "/" << config.db_schema
            << std::endl;

  db::ConnectionPool pool(config);
  repository::TaskRepository repo(pool);

  // HTTP用io_context(async I/O、CPUコア数程度のスレッド)と、
  // DB用thread_pool(ブロッキング呼び出し許容、コネクションプールと同数)を分離する
  // (README.md「同期DBアクセスの隔離」節、このアーキテクチャの核)
  asio::io_context io_ctx;
  asio::thread_pool db_pool(config.db_pool_size);

  // 3issuerのJWT検証Dispatcher(backend-rustのsrc/auth/mod.rsと同じ構成)
  auth::Dispatcher dispatcher;
  dispatcher.Register(auth::kLocalHmacIssuer,
                       std::make_shared<auth::HmacVerifier>(
                           config.local_hmac_secret, auth::kLocalHmacIssuer, config.expected_audience));
  dispatcher.Register(auth::kLocalRsaIssuer,
                       std::make_shared<auth::JwksVerifier>(
                           config.local_rsa_jwks_url, auth::kLocalRsaIssuer, config.expected_audience));
  dispatcher.Register(config.keycloak_issuer,
                       std::make_shared<auth::JwksVerifier>(
                           config.keycloak_issuer + "/protocol/openid-connect/certs",
                           config.keycloak_issuer, config.expected_audience));

  // feature_flagsテーブルの直接ポーリング(10秒間隔固定、README.md参照)
  flags::FeatureFlagPoller flag_poller(config);
  flag_poller.Start();

  application::TaskHandler task_handler(repo, db_pool, dispatcher);
  http::Router router;
  router.Add("GET", "/internal/v1/tasks", false,
             [&task_handler](const http::HttpRequest& req, int64_t id) {
               return task_handler.List(req, id);
             });
  router.Add("GET", "/internal/v1/tasks", true,
             [&task_handler](const http::HttpRequest& req, int64_t id) {
               return task_handler.Get(req, id);
             });
  router.Add("POST", "/internal/v1/tasks", false,
             [&task_handler](const http::HttpRequest& req, int64_t id) {
               return task_handler.Create(req, id);
             });
  router.Add("PATCH", "/internal/v1/tasks", true,
             [&task_handler](const http::HttpRequest& req, int64_t id) {
               return task_handler.Update(req, id);
             });
  router.Add("DELETE", "/internal/v1/tasks", true,
             [&task_handler](const http::HttpRequest& req, int64_t id) {
               return task_handler.Delete(req, id);
             });

  const auto port = static_cast<unsigned short>(std::stoi(config.http_addr));
  tcp::acceptor acceptor(io_ctx, {tcp::v4(), port});
  asio::co_spawn(io_ctx, http::RunListener(acceptor, router), asio::detached);

  // 外部公開API(:8109、bffを経由しない別listener、CONTRACT.mdセクション11)
  external::ExternalHandler external_handler(repo, db_pool, dispatcher, flag_poller,
                                              config.external_api_client_id);
  http::Router external_router;
  external_router.Add("GET", "/external/v1/tasks", false,
                       [&external_handler](const http::HttpRequest& req, int64_t id) {
                         return external_handler.List(req, id);
                       });
  const auto external_port = static_cast<unsigned short>(std::stoi(config.external_http_addr));
  tcp::acceptor external_acceptor(io_ctx, {tcp::v4(), external_port});
  asio::co_spawn(io_ctx, http::RunListener(external_acceptor, external_router), asio::detached);

  // gRPC v2(:9099)。gRPC C++は自前のスレッドプール(Callback API用)を持つため、
  // Boost.Asioのio_contextとは別に、gRPCサーバー自体を別スレッドでRunさせる
  // (README.md「gRPCのスレッドモデル」節参照)
  grpcservice::TaskGrpcServiceImpl grpc_service(repo, dispatcher);
  grpc::ServerBuilder grpc_builder;
  const std::string grpc_listen_addr = "0.0.0.0:" + config.grpc_addr;
  grpc_builder.AddListeningPort(grpc_listen_addr, grpc::InsecureServerCredentials());
  grpc_builder.RegisterService(&grpc_service);
  std::unique_ptr<grpc::Server> grpc_server = grpc_builder.BuildAndStart();
  std::thread grpc_thread([&grpc_server] { grpc_server->Wait(); });

  std::cout << "backend-cpp listening: REST=:" << config.http_addr
            << " EXTERNAL=:" << config.external_http_addr << " GRPC=:" << config.grpc_addr
            << std::endl;

  asio::signal_set signals(io_ctx, SIGINT, SIGTERM);
  signals.async_wait([&](auto, auto) {
    std::cout << "shutting down..." << std::endl;
    io_ctx.stop();
    db_pool.stop();
    flag_poller.Stop();
    grpc_server->Shutdown();
  });

  // io_contextはCPUコア数程度のスレッドで回す(README.md参照)
  const unsigned n = std::max(1u, std::thread::hardware_concurrency());
  std::vector<std::thread> threads;
  for (unsigned i = 0; i + 1 < n; ++i) {
    threads.emplace_back([&io_ctx] { io_ctx.run(); });
  }
  io_ctx.run();
  for (auto& t : threads) t.join();
  db_pool.join();
  grpc_thread.join();
  return 0;
}
