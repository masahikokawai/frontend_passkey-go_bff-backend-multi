#pragma once

#include <atomic>
#include <boost/asio/io_context.hpp>
#include <boost/asio/ip/tcp.hpp>
#include <mutex>
#include <string>
#include <thread>

namespace backend_cpp::testutil {

// テスト専用の最小限のJWKSモックサーバー。auth::JwksVerifierのHttpGet()と同じく
// 素朴な同期HTTP(Boost::Beast)でGETを1本ずつ処理する(並行性は不要、テストでは
// JwksVerifier自身が単一スレッドから逐次リクエストするため十分)
class MockJwksServer {
 public:
  // portで即座にlistenを開始する。bodyは/.well-known/jwks.json相当のJSON文字列
  MockJwksServer(unsigned short port, std::string body);
  ~MockJwksServer();
  MockJwksServer(const MockJwksServer&) = delete;

  // 動的にJWKSの中身を差し替える(「更新後は別の鍵が返る」ようなテストに使う)
  void SetBody(std::string body);

  std::string Url() const;

 private:
  void Run();

  unsigned short port_;
  std::string body_;
  std::mutex body_mutex_;
  boost::asio::io_context ioc_;
  boost::asio::ip::tcp::acceptor acceptor_;
  std::thread thread_;
  std::atomic<bool> stop_{false};
};

}  // namespace backend_cpp::testutil
