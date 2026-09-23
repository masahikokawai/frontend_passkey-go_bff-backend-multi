#include "mock_jwks_server.hpp"

#include <boost/beast/core.hpp>
#include <boost/beast/http.hpp>
#include <iostream>

namespace backend_cpp::testutil {

namespace beast = boost::beast;
namespace http = boost::beast::http;
using boost::asio::ip::tcp;

MockJwksServer::MockJwksServer(unsigned short port, std::string body)
    : port_(port), body_(std::move(body)), acceptor_(ioc_, {tcp::v4(), port}) {
  thread_ = std::thread([this] { Run(); });
}

MockJwksServer::~MockJwksServer() {
  stop_ = true;
  boost::system::error_code ec;
  acceptor_.close(ec);  // ブロッキング中のaccept()を解除する
  if (thread_.joinable()) thread_.join();
}

void MockJwksServer::SetBody(std::string body) {
  std::lock_guard<std::mutex> lock(body_mutex_);
  body_ = std::move(body);
}

std::string MockJwksServer::Url() const {
  return "http://127.0.0.1:" + std::to_string(port_) + "/.well-known/jwks.json";
}

void MockJwksServer::Run() {
  while (!stop_) {
    boost::system::error_code ec;
    tcp::socket socket(ioc_);
    acceptor_.accept(socket, ec);
    if (ec) break;  // acceptor_.close()によるキャンセル、または他のエラー→ループ終了

    beast::flat_buffer buffer;
    http::request<http::string_body> req;
    boost::system::error_code read_ec;
    http::read(socket, buffer, req, read_ec);
    if (read_ec) continue;

    std::string body_copy;
    {
      std::lock_guard<std::mutex> lock(body_mutex_);
      body_copy = body_;
    }

    http::response<http::string_body> res{http::status::ok, req.version()};
    res.set(http::field::content_type, "application/json");
    res.keep_alive(false);
    res.body() = body_copy;
    res.prepare_payload();

    boost::system::error_code write_ec;
    http::write(socket, res, write_ec);
    socket.shutdown(tcp::socket::shutdown_send, ec);
  }
}

}  // namespace backend_cpp::testutil
