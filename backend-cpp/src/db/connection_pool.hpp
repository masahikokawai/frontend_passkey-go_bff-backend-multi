#pragma once

#include <condition_variable>
#include <memory>
#include <mutex>
#include <queue>

#include <mysql/mysql.h>

#include "config.hpp"

namespace backend_cpp::db {

// libmysqlclient(classic C API)をRAIIで包む。mysql-connector-c++はX DevAPI
// (X Protocol、既定ポート33060)のみ提供するが、docker-compose.yamlはclassic protocol
// (:3306→13306)しか公開しておらず接続できないため、classic protocolで話せるlibmysqlclientを
// 採用した(README.md「アーキテクチャ選定」節参照)。副次的に、将来のC実装も同じ
// libmysqlclientを使うことになるため、「同じCライブラリをC++がRAIIでどう安全に包むか」
// という、当初想定していたより一段クリーンな比較教材になる
struct MysqlDeleter {
  void operator()(MYSQL* conn) const {
    if (conn) mysql_close(conn);
  }
};
using MysqlConnPtr = std::unique_ptr<MYSQL, MysqlDeleter>;

class ConnectionPool {
 public:
  explicit ConnectionPool(const Config& config);

  // RAIIラッパー: デストラクタで自動的にプールへ返却する(生成/破棄をペアで手動管理する
  // C言語との対比ポイント、README.md参照)
  class Lease {
   public:
    Lease(ConnectionPool& pool, MysqlConnPtr conn) : pool_(pool), conn_(std::move(conn)) {}
    ~Lease() {
      if (conn_) pool_.Release(std::move(conn_));
    }
    Lease(const Lease&) = delete;
    Lease& operator=(const Lease&) = delete;
    Lease(Lease&&) = default;

    MYSQL* get() { return conn_.get(); }

   private:
    ConnectionPool& pool_;
    MysqlConnPtr conn_;
  };

  // 空きが無ければ空きが出るまでブロックする(呼び出しはdb_thread_pool上で行うこと、
  // io_contextのスレッドから直接呼ばない、README.md参照)
  Lease Acquire();

 private:
  MysqlConnPtr Connect();
  void Release(MysqlConnPtr conn);

  Config config_;
  std::mutex mutex_;
  std::condition_variable cv_;
  std::queue<MysqlConnPtr> idle_;
  int outstanding_ = 0;
  int max_size_;
};

}  // namespace backend_cpp::db
