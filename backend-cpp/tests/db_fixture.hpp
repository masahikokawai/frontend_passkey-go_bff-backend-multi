#pragma once

#include <cstdint>
#include <optional>
#include <string>

#include "db/connection_pool.hpp"

namespace backend_cpp::testutil {

// テスト用に一意な(email/keycloak_subが衝突しない)usersぶんを作成し、
// スコープを抜けるときに関連するtasks/task_labels/user_keycloaks/usersを削除する
// (backend-c/backend-rustのテストで確立した「使い捨てユーザーを作って片付ける」方針と同じ)
class TestUser {
 public:
  // keycloak_subを指定するとuser_keycloaksにも行を作る(Keycloak発行issuerのuser_id解決テスト用)
  explicit TestUser(db::ConnectionPool& pool, std::optional<std::string> keycloak_sub = std::nullopt);
  ~TestUser();
  TestUser(const TestUser&) = delete;

  int64_t id() const { return id_; }

 private:
  db::ConnectionPool& pool_;
  int64_t id_ = 0;
};

// テスト用のlabelを作成し、破棄時に削除する
class TestLabel {
 public:
  explicit TestLabel(db::ConnectionPool& pool);
  ~TestLabel();
  TestLabel(const TestLabel&) = delete;

  int64_t id() const { return id_; }

 private:
  db::ConnectionPool& pool_;
  int64_t id_ = 0;
};

}  // namespace backend_cpp::testutil
