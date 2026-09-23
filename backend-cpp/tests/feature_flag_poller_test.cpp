#include "flags/feature_flag_poller.hpp"

#include <gtest/gtest.h>
#include <mysql/mysql.h>

#include <chrono>
#include <stdexcept>
#include <thread>

#include "config.hpp"
#include "db/connection_pool.hpp"

// FeatureFlagPoller::Variation()のフォールバック挙動の結合テスト。実DBに使い捨てのflag_key
// を作って確認する(共有flagのbackend.external-tasks-pagination-v2には触れない、
// external_handler_integration_test.cpp側がそれを使うため衝突を避ける)
namespace backend_cpp::flags {
namespace {

std::string UniqueFlagKey(const std::string& suffix) {
  auto now_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
                    std::chrono::system_clock::now().time_since_epoch())
                    .count();
  return "test.cpp-poller-" + suffix + "-" + std::to_string(now_ns);
}

class TestFlagRow {
 public:
  TestFlagRow(db::ConnectionPool& pool, std::string flag_key, bool enabled,
              const std::string& default_variation)
      : pool_(pool), flag_key_(std::move(flag_key)) {
    auto lease = pool_.Acquire();
    MYSQL* conn = lease.get();
    std::string sql = "INSERT INTO feature_flags (flag_key, description, default_variation, "
                       "enabled, created_at, updated_at) VALUES ('" +
                       flag_key_ + "', 'backend-cpp test', '" + default_variation + "', " +
                       (enabled ? "1" : "0") + ", NOW(), NOW())";
    if (mysql_query(conn, sql.c_str()) != 0) throw std::runtime_error(mysql_error(conn));
  }

  ~TestFlagRow() {
    try {
      auto lease = pool_.Acquire();
      MYSQL* conn = lease.get();
      std::string sql = "DELETE FROM feature_flags WHERE flag_key = '" + flag_key_ + "'";
      mysql_query(conn, sql.c_str());
    } catch (...) {
    }
  }

  const std::string& key() const { return flag_key_; }

 private:
  db::ConnectionPool& pool_;
  std::string flag_key_;
};

class FeatureFlagPollerTest : public ::testing::Test {
 protected:
  void SetUp() override {
    config_ = Config::FromEnv();
    pool_ = std::make_unique<db::ConnectionPool>(config_);
  }

  // ポーラーを起動し、最初のポーリング(Start()直後に即座に1回走る)が終わるまで待つ
  std::unique_ptr<FeatureFlagPoller> StartedPoller() {
    auto poller = std::make_unique<FeatureFlagPoller>(config_);
    poller->Start();
    std::this_thread::sleep_for(std::chrono::milliseconds(500));
    return poller;
  }

  Config config_;
  std::unique_ptr<db::ConnectionPool> pool_;
};

TEST_F(FeatureFlagPollerTest, VariationReturnsDefaultVariationWhenEnabled) {
  TestFlagRow flag(*pool_, UniqueFlagKey("enabled"), true, "on");
  auto poller = StartedPoller();
  EXPECT_EQ(poller->Variation(flag.key(), "off"), "on");
}

TEST_F(FeatureFlagPollerTest, VariationReturnsFallbackWhenDisabled) {
  TestFlagRow flag(*pool_, UniqueFlagKey("disabled"), false, "on");
  auto poller = StartedPoller();
  EXPECT_EQ(poller->Variation(flag.key(), "off"), "off");
}

TEST_F(FeatureFlagPollerTest, VariationReturnsFallbackWhenNotFound) {
  auto poller = StartedPoller();
  EXPECT_EQ(poller->Variation("no-such-flag-key-at-all", "the-default"), "the-default");
}

}  // namespace
}  // namespace backend_cpp::flags
