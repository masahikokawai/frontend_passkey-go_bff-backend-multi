#pragma once

#include <atomic>
#include <map>
#include <mutex>
#include <string>
#include <thread>

#include "config.hpp"

namespace backend_cpp::flags {

// backend(Go)・backend-rustのsrc/flags.rsと同じ設計: 自分専用のMySQL接続で
// feature_flagsテーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない)
// 外部公開API(backend.external-tasks-pagination-v2)の判定に使う
class FeatureFlagPoller {
 public:
  explicit FeatureFlagPoller(Config config);
  ~FeatureFlagPoller();
  FeatureFlagPoller(const FeatureFlagPoller&) = delete;

  void Start();
  void Stop();

  // enabled=falseの場合、または未知のflag_keyの場合はdefault_valueを返す
  std::string Variation(const std::string& flag_key, const std::string& default_value);

 private:
  void Run();
  void PollOnce();

  Config config_;
  std::thread thread_;
  std::atomic<bool> stop_{false};
  std::mutex mutex_;
  struct Entry {
    bool enabled;
    std::string default_variation;
  };
  std::map<std::string, Entry> entries_;
};

}  // namespace backend_cpp::flags
