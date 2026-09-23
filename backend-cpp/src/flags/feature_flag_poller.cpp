#include "flags/feature_flag_poller.hpp"

#include <mysql/mysql.h>

#include <chrono>
#include <cstring>
#include <iostream>

namespace backend_cpp::flags {

FeatureFlagPoller::FeatureFlagPoller(Config config) : config_(std::move(config)) {}

FeatureFlagPoller::~FeatureFlagPoller() { Stop(); }

void FeatureFlagPoller::Start() {
  thread_ = std::thread([this] { Run(); });
}

void FeatureFlagPoller::Stop() {
  stop_ = true;
  if (thread_.joinable()) thread_.join();
}

void FeatureFlagPoller::Run() {
  // 初回は即座にポーリングし、以後10秒間隔(backend-rustのsrc/flags.rsと同じ固定値)
  while (!stop_) {
    PollOnce();
    for (int i = 0; i < 100 && !stop_; ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
  }
}

void FeatureFlagPoller::PollOnce() {
  MYSQL* conn = mysql_init(nullptr);
  if (!conn) return;
  if (!mysql_real_connect(conn, config_.db_host.c_str(), config_.db_user.c_str(),
                           config_.db_password.empty() ? nullptr : config_.db_password.c_str(),
                           config_.db_schema.c_str(), config_.db_port, nullptr, 0)) {
    std::cerr << "level=ERROR msg=\"feature_flagsポーリング用の接続に失敗\" error=\""
              << mysql_error(conn) << "\"" << std::endl;
    mysql_close(conn);
    return;
  }

  const char* query = "SELECT flag_key, enabled, default_variation FROM feature_flags";
  if (mysql_real_query(conn, query, static_cast<unsigned long>(std::strlen(query))) != 0) {
    std::cerr << "level=ERROR msg=\"feature_flagsクエリ失敗\" error=\"" << mysql_error(conn)
              << "\"" << std::endl;
    mysql_close(conn);
    return;
  }
  MYSQL_RES* res = mysql_store_result(conn);
  if (!res) {
    mysql_close(conn);
    return;
  }

  std::map<std::string, Entry> new_entries;
  MYSQL_ROW row;
  while ((row = mysql_fetch_row(res)) != nullptr) {
    std::string key = row[0] ? row[0] : "";
    bool enabled = row[1] && std::string(row[1]) == "1";
    std::string variation = row[2] ? row[2] : "";
    new_entries[key] = Entry{enabled, variation};
  }
  mysql_free_result(res);
  mysql_close(conn);

  std::lock_guard<std::mutex> lock(mutex_);
  entries_ = std::move(new_entries);
}

std::string FeatureFlagPoller::Variation(const std::string& flag_key,
                                          const std::string& default_value) {
  std::lock_guard<std::mutex> lock(mutex_);
  auto it = entries_.find(flag_key);
  if (it == entries_.end() || !it->second.enabled) return default_value;
  return it->second.default_variation;
}

}  // namespace backend_cpp::flags
