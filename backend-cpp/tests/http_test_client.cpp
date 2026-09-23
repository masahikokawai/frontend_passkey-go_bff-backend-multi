#include "http_test_client.hpp"

#include <boost/asio/connect.hpp>
#include <boost/asio/io_context.hpp>
#include <boost/asio/ip/tcp.hpp>
#include <boost/beast/core.hpp>
#include <boost/beast/http.hpp>

namespace backend_cpp::testutil {

namespace beast = boost::beast;
namespace http = boost::beast::http;
namespace asio = boost::asio;
using asio::ip::tcp;

HttpTestResponse HttpGetWithHeaders(const std::string& host, unsigned short port,
                                     const std::string& target,
                                     const std::map<std::string, std::string>& headers) {
  asio::io_context ioc;
  tcp::resolver resolver(ioc);
  beast::tcp_stream stream(ioc);

  auto const results = resolver.resolve(host, std::to_string(port));
  stream.connect(results);

  http::request<http::string_body> req{http::verb::get, target, 11};
  req.set(http::field::host, host);
  req.set(http::field::user_agent, "backend-cpp-test");
  for (const auto& [key, value] : headers) {
    req.set(key, value);
  }
  http::write(stream, req);

  beast::flat_buffer buffer;
  http::response<http::string_body> res;
  http::read(stream, buffer, res);

  beast::error_code ec;
  stream.socket().shutdown(tcp::socket::shutdown_both, ec);

  return HttpTestResponse{static_cast<int>(res.result_int()), res.body()};
}

}  // namespace backend_cpp::testutil
