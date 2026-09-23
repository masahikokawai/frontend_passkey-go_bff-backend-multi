#pragma once

#include <boost/asio/awaitable.hpp>
#include <boost/asio/co_spawn.hpp>
#include <boost/asio/thread_pool.hpp>
#include <boost/asio/use_awaitable.hpp>
#include <type_traits>
#include <utility>

namespace backend_cpp::db {

namespace asio = boost::asio;

// io_context(HTTP用)のスレッドをブロックせずに、db_pool(DB用thread_pool)上でfuncを
// 実行し、完了を待つ。README.md「同期DBアクセスの隔離」節で説明した設計そのもの
template <typename Func>
asio::awaitable<std::invoke_result_t<Func>> RunBlocking(asio::thread_pool& db_pool, Func func) {
  co_return co_await asio::co_spawn(
      db_pool,
      [func = std::move(func)]() -> asio::awaitable<std::invoke_result_t<Func>> {
        co_return func();  // ここはdb_poolのスレッドで実行される(ブロッキングDB呼び出しOK)
      },
      asio::use_awaitable);
}

}  // namespace backend_cpp::db
