#include "db_fixture.hpp"

#include <mysql/mysql.h>

#include <atomic>
#include <chrono>
#include <cstdio>
#include <stdexcept>

namespace backend_cpp::testutil {

namespace {

std::string UniqueSuffix() {
  static std::atomic<int64_t> counter{0};
  auto now_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
                    std::chrono::system_clock::now().time_since_epoch())
                    .count();
  return std::to_string(now_ns) + "-" + std::to_string(counter.fetch_add(1));
}

void MustExec(MYSQL* conn, const std::string& sql) {
  if (mysql_query(conn, sql.c_str()) != 0) {
    throw std::runtime_error(std::string("query failed: ") + mysql_error(conn) + " (" + sql + ")");
  }
}

// エスケープ済みのSQLリテラル文字列を組み立てる(このテストヘルパー限定の簡易版、
// プレースホルダのprepared statementを使うほどの複雑さが無いため生SQLで組み立てる)
std::string Escape(MYSQL* conn, const std::string& s) {
  std::string out(s.size() * 2 + 1, '\0');
  unsigned long len = mysql_real_escape_string(conn, out.data(), s.c_str(), s.size());
  out.resize(len);
  return out;
}

}  // namespace

TestUser::TestUser(db::ConnectionPool& pool, std::optional<std::string> keycloak_sub) : pool_(pool) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  std::string suffix = UniqueSuffix();
  std::string email = "backend-cpp-test-" + suffix + "@example.com";
  std::string name = "backend-cpp-test-" + suffix;
  MustExec(conn, "INSERT INTO users (email, name, role, created_at, updated_at) VALUES ('" +
                     Escape(conn, email) + "', '" + Escape(conn, name) + "', 1, NOW(), NOW())");
  id_ = static_cast<int64_t>(mysql_insert_id(conn));

  if (keycloak_sub.has_value()) {
    MustExec(conn, "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) "
                   "VALUES (" +
                       std::to_string(id_) + ", '" + Escape(conn, *keycloak_sub) +
                       "', NOW(), NOW())");
  }
}

TestUser::~TestUser() {
  if (id_ == 0) return;
  try {
    auto lease = pool_.Acquire();
    MYSQL* conn = lease.get();
    MustExec(conn, "DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE user_id = " +
                       std::to_string(id_) + ")");
    MustExec(conn, "DELETE FROM tasks WHERE user_id = " + std::to_string(id_));
    MustExec(conn, "DELETE FROM user_keycloaks WHERE user_id = " + std::to_string(id_));
    MustExec(conn, "DELETE FROM users WHERE id = " + std::to_string(id_));
  } catch (...) {
    // 後片付けの失敗でテスト自体を落とさない(ベストエフォート)
  }
}

TestLabel::TestLabel(db::ConnectionPool& pool) : pool_(pool) {
  auto lease = pool_.Acquire();
  MYSQL* conn = lease.get();
  std::string name = "backend-cpp-test-label-" + UniqueSuffix();
  MustExec(conn, "INSERT INTO labels (name, created_at, updated_at) VALUES ('" +
                     Escape(conn, name) + "', NOW(), NOW())");
  id_ = static_cast<int64_t>(mysql_insert_id(conn));
}

TestLabel::~TestLabel() {
  if (id_ == 0) return;
  try {
    auto lease = pool_.Acquire();
    MYSQL* conn = lease.get();
    MustExec(conn, "DELETE FROM task_labels WHERE label_id = " + std::to_string(id_));
    MustExec(conn, "DELETE FROM labels WHERE id = " + std::to_string(id_));
  } catch (...) {
  }
}

}  // namespace backend_cpp::testutil
