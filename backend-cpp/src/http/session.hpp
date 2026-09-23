#pragma once

#include <boost/asio/awaitable.hpp>
#include <boost/asio/co_spawn.hpp>
#include <boost/asio/detached.hpp>
#include <boost/asio/ip/tcp.hpp>

#include "http/router.hpp"

namespace backend_cpp::http {

namespace asio = boost::asio;
using asio::ip::tcp;

// io_context上でacceptループを回し、接続ごとにコルーチンをco_spawnする
// (Beastにはサーバーという概念自体が無く、これが定石のセッション管理パターン、README.md参照)
asio::awaitable<void> RunListener(tcp::acceptor& acceptor, Router& router);

}  // namespace backend_cpp::http
