#include "db/connection_pool.hpp"

#include <stdexcept>

namespace backend_cpp::db {

ConnectionPool::ConnectionPool(const Config& config)
    : config_(config), max_size_(config.db_pool_size) {
  // 事前に1本だけ張って設定ミスを早期検出する(mysql_library_init相当はlibmysqlclientが
  // 内部で暗黙的に行うためここでは呼ばない)
  auto conn = Connect();
  idle_.push(std::move(conn));
}

MysqlConnPtr ConnectionPool::Connect() {
  MYSQL* raw = mysql_init(nullptr);
  if (!raw) throw std::runtime_error("mysql_init failed");
  MysqlConnPtr conn(raw);

  // MYSQL_OPT_RECONNECT: 学習用途で長時間起動したままにしても、アイドルタイムアウトで
  // 切れた接続を自動再接続する(本番ならプール側でヘルスチェックすべきだが、今回は簡略化)
  bool reconnect = true;
  mysql_options(conn.get(), MYSQL_OPT_RECONNECT, &reconnect);

  if (!mysql_real_connect(conn.get(), config_.db_host.c_str(), config_.db_user.c_str(),
                           config_.db_password.empty() ? nullptr : config_.db_password.c_str(),
                           config_.db_schema.c_str(), config_.db_port, nullptr, 0)) {
    std::string err = mysql_error(conn.get());
    throw std::runtime_error("mysql_real_connect failed: " + err);
  }
  // このプロジェクトのMySQLはutf8mb4(docker-compose.yaml参照)
  mysql_set_character_set(conn.get(), "utf8mb4");
  return conn;
}

ConnectionPool::Lease ConnectionPool::Acquire() {
  std::unique_lock<std::mutex> lock(mutex_);
  if (!idle_.empty()) {
    auto conn = std::move(idle_.front());
    idle_.pop();
    ++outstanding_;
    return Lease(*this, std::move(conn));
  }
  if (outstanding_ < max_size_) {
    ++outstanding_;
    lock.unlock();
    return Lease(*this, Connect());
  }
  // 上限に達している場合は、db_thread_pool上のこのスレッドをここでブロックして待つ
  // (io_contextのスレッドをブロックしないことが設計上の前提、README.md参照)
  cv_.wait(lock, [this] { return !idle_.empty(); });
  auto conn = std::move(idle_.front());
  idle_.pop();
  ++outstanding_;
  return Lease(*this, std::move(conn));
}

void ConnectionPool::Release(MysqlConnPtr conn) {
  std::lock_guard<std::mutex> lock(mutex_);
  --outstanding_;
  idle_.push(std::move(conn));
  cv_.notify_one();
}

}  // namespace backend_cpp::db
