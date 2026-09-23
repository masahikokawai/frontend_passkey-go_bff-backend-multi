#include "http/session.hpp"

#include <boost/beast/core.hpp>
#include <boost/beast/http.hpp>
#include <iostream>

namespace backend_cpp::http {

namespace beast = boost::beast;
namespace http_beast = boost::beast::http;

namespace {

HttpRequest ToHttpRequest(const http_beast::request<http_beast::string_body>& req) {
  HttpRequest out;
  out.method = std::string(http_beast::to_string(req.method()));
  out.target = std::string(req.target());
  out.body = req.body();
  for (const auto& field : req) {
    std::string name(field.name_string());
    for (auto& c : name) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    out.headers[name] = std::string(field.value());
  }
  return out;
}

http_beast::response<http_beast::string_body> ToBeastResponse(
    const HttpResponse& resp, const http_beast::request<http_beast::string_body>& req) {
  http_beast::response<http_beast::string_body> res{
      static_cast<http_beast::status>(resp.status), req.version()};
  res.set(http_beast::field::server, "backend-cpp");
  res.set(http_beast::field::content_type, resp.content_type);
  res.keep_alive(req.keep_alive());
  res.body() = resp.body;
  res.prepare_payload();
  return res;
}

// 1コネクション分のセッション。Beastには「サーバー」という概念自体が無いため、
// 接続を受けるたびにこのコルーチンをco_spawnする(README.md「Beastのセッション管理」参照)
asio::awaitable<void> RunSession(tcp::socket socket, Router& router) {
  beast::tcp_stream stream(std::move(socket));
  beast::flat_buffer buffer;

  try {
    for (;;) {
      http_beast::request<http_beast::string_body> req;
      stream.expires_after(std::chrono::seconds(30));
      co_await http_beast::async_read(stream, buffer, req, asio::use_awaitable);

      HttpRequest simple_req = ToHttpRequest(req);
      HttpResponse simple_resp = co_await router.Dispatch(simple_req);
      auto res = ToBeastResponse(simple_resp, req);

      const bool keep_alive = res.keep_alive();
      co_await http_beast::async_write(stream, res, asio::use_awaitable);
      if (!keep_alive) break;
    }
  } catch (const boost::system::system_error& e) {
    // 接続が切れた(EOF等)場合はログに出さず静かに終了する。それ以外は標準エラーへ出す
    if (e.code() != http_beast::error::end_of_stream) {
      std::cerr << "session error: " << e.what() << std::endl;
    }
  } catch (const std::exception& e) {
    std::cerr << "session error: " << e.what() << std::endl;
  }

  beast::error_code ec;
  stream.socket().shutdown(tcp::socket::shutdown_send, ec);
}

}  // namespace

asio::awaitable<void> RunListener(tcp::acceptor& acceptor, Router& router) {
  for (;;) {
    tcp::socket socket = co_await acceptor.async_accept(asio::use_awaitable);
    auto executor = socket.get_executor();
    asio::co_spawn(executor, RunSession(std::move(socket), router), asio::detached);
  }
}

}  // namespace backend_cpp::http
