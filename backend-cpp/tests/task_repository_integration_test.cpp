#include "repository/task_repository.hpp"

#include <gtest/gtest.h>
#include <mysql/mysql.h>

#include <chrono>
#include <memory>
#include <string>
#include <vector>

#include "config.hpp"
#include "db/connection_pool.hpp"
#include "db_fixture.hpp"
#include "domain/task.hpp"

// 実DB(docker-compose上のMySQL)に接続する結合テスト。ctestには登録しない
// (README.md「結合テスト」節参照、手動実行のみ)
namespace backend_cpp::repository {
namespace {

using backend_cpp::domain::TaskInput;
using backend_cpp::domain::TaskStatus;
using backend_cpp::testutil::TestLabel;
using backend_cpp::testutil::TestUser;

class TaskRepositoryIntegrationTest : public ::testing::Test {
 protected:
  void SetUp() override {
    config_ = Config::FromEnv();
    pool_ = std::make_unique<db::ConnectionPool>(config_);
  }

  Config config_;
  std::unique_ptr<db::ConnectionPool> pool_;
};

TaskInput MakeInput(const std::string& name, const std::string& finished_on,
                     std::vector<int64_t> label_ids = {}) {
  TaskInput input;
  input.name = name;
  input.status_raw = "waiting";
  input.finished_on = finished_on;
  input.label_ids = std::move(label_ids);
  return input;
}

TEST_F(TaskRepositoryIntegrationTest, CreateFindUpdateDeleteRoundTrip) {
  TestUser user(*pool_);
  TaskRepository repo(*pool_);

  auto created = repo.Create(user.id(), MakeInput("cpp-it-1", "2099-01-01"), TaskStatus::kWaiting);
  ASSERT_TRUE(created.has_value());
  int64_t task_id = *created;

  auto found = repo.FindById(task_id, user.id());
  ASSERT_TRUE(found.has_value());
  EXPECT_EQ(found->name, "cpp-it-1");
  EXPECT_EQ(found->status, TaskStatus::kWaiting);

  auto updated = repo.Update(task_id, user.id(), MakeInput("cpp-it-1-updated", "2099-02-02"),
                              TaskStatus::kCompleted);
  ASSERT_TRUE(updated.has_value());
  EXPECT_TRUE(*updated);

  auto refetched = repo.FindById(task_id, user.id());
  ASSERT_TRUE(refetched.has_value());
  EXPECT_EQ(refetched->name, "cpp-it-1-updated");
  EXPECT_EQ(refetched->status, TaskStatus::kCompleted);

  auto deleted = repo.Delete(task_id, user.id());
  ASSERT_TRUE(deleted.has_value());
  EXPECT_TRUE(*deleted);

  auto after_delete = repo.FindById(task_id, user.id());
  EXPECT_FALSE(after_delete.has_value());

  // 冪等性: 既に削除済みのidをもう一度削除してもfalse(見つからない)を返すだけで、
  // エラーにはならない
  auto delete_again = repo.Delete(task_id, user.id());
  ASSERT_TRUE(delete_again.has_value());
  EXPECT_FALSE(*delete_again);
}

// Deleteがtasksとtask_labelsの両方を1トランザクションで削除することを確認する
// (task_labelsに外部キー制約は無いため、トランザクション無しだと孤立行が残り得る。
// backend-rustで見つかった既知バグと同種、README.md参照)
TEST_F(TaskRepositoryIntegrationTest, DeleteRemovesTaskLabelsRows) {
  TestUser user(*pool_);
  TestLabel label(*pool_);
  TaskRepository repo(*pool_);

  auto created = repo.Create(user.id(), MakeInput("cpp-it-2", "2099-01-01", {label.id()}),
                              TaskStatus::kWaiting);
  ASSERT_TRUE(created.has_value());
  int64_t task_id = *created;

  auto found = repo.FindById(task_id, user.id());
  ASSERT_TRUE(found.has_value());
  ASSERT_EQ(found->labels.size(), 1u);

  auto deleted = repo.Delete(task_id, user.id());
  ASSERT_TRUE(deleted.has_value());
  EXPECT_TRUE(*deleted);

  // task_labels側が孤立していないことをSQLで直接確認する
  auto lease = pool_->Acquire();
  MYSQL* conn = lease.get();
  std::string sql = "SELECT COUNT(*) FROM task_labels WHERE task_id = " + std::to_string(task_id);
  ASSERT_EQ(mysql_query(conn, sql.c_str()), 0);
  MYSQL_RES* res = mysql_store_result(conn);
  ASSERT_NE(res, nullptr);
  MYSQL_ROW row = mysql_fetch_row(res);
  ASSERT_NE(row, nullptr);
  EXPECT_STREQ(row[0], "0");
  mysql_free_result(res);
}

// 【既知バグの回帰防止】同一リクエスト内の重複label_id(例: [x,x,y])は重複排除されてから
// 書き込まれる(CONTRACT.mdセクション23.1、5言語中4言語で見つかった実バグと同種)
TEST_F(TaskRepositoryIntegrationTest, CreateDedupsDuplicateLabelIds) {
  TestUser user(*pool_);
  TestLabel label_a(*pool_);
  TestLabel label_b(*pool_);
  TaskRepository repo(*pool_);

  auto created = repo.Create(
      user.id(), MakeInput("cpp-it-3", "2099-01-01", {label_a.id(), label_a.id(), label_b.id()}),
      TaskStatus::kWaiting);
  ASSERT_TRUE(created.has_value());

  auto found = repo.FindById(*created, user.id());
  ASSERT_TRUE(found.has_value());
  EXPECT_EQ(found->labels.size(), 2u);
}

TEST_F(TaskRepositoryIntegrationTest, OtherUserCannotSeeTask) {
  TestUser owner(*pool_);
  TestUser other(*pool_);
  TaskRepository repo(*pool_);

  auto created = repo.Create(owner.id(), MakeInput("cpp-it-4", "2099-01-01"), TaskStatus::kWaiting);
  ASSERT_TRUE(created.has_value());

  auto found_by_other = repo.FindById(*created, other.id());
  EXPECT_FALSE(found_by_other.has_value());

  auto deleted_by_other = repo.Delete(*created, other.id());
  ASSERT_TRUE(deleted_by_other.has_value());
  EXPECT_FALSE(*deleted_by_other);  // 他人のtaskは「見つからない」扱いで削除もできない
}

TEST_F(TaskRepositoryIntegrationTest, FindUserById) {
  TestUser user(*pool_);
  TaskRepository repo(*pool_);

  auto found = repo.FindUserById(user.id());
  ASSERT_TRUE(found.has_value());
  EXPECT_EQ(*found, user.id());

  auto not_found = repo.FindUserById(-1);
  EXPECT_FALSE(not_found.has_value());
}

TEST_F(TaskRepositoryIntegrationTest, FindUserIdByKeycloakSub) {
  std::string sub = "cpp-it-keycloak-sub-" +
                     std::to_string(std::chrono::system_clock::now().time_since_epoch().count());
  TestUser user(*pool_, sub);
  TaskRepository repo(*pool_);

  auto found = repo.FindUserIdByKeycloakSub(sub);
  ASSERT_TRUE(found.has_value());
  EXPECT_EQ(*found, user.id());

  auto not_found = repo.FindUserIdByKeycloakSub("no-such-sub");
  EXPECT_FALSE(not_found.has_value());
}

}  // namespace
}  // namespace backend_cpp::repository
