#pragma once

#include <cstdint>
#include <optional>
#include <string>
#include <vector>

namespace backend_cpp::domain {

// backend/internal/model/enum.go の TaskStatus と同じ数値対応(waiting=1/work_in_progress=2/completed=3)
enum class TaskStatus : uint8_t {
  kWaiting = 1,
  kWorkInProgress = 2,
  kCompleted = 3,
};

std::optional<TaskStatus> StatusFromString(const std::string& s);
std::string StatusToString(TaskStatus status);

struct Label {
  int64_t id;
  std::string name;
};

// protobuf生成クラスとは独立したドメインモデル(次フェーズでgRPCを追加した際、
// Mapperを介して相互変換する。protoの生成型をそのままドメインに使わない)
struct Task {
  int64_t id = 0;
  std::string name;
  std::optional<std::string> description;
  TaskStatus status = TaskStatus::kWaiting;
  std::string finished_on;  // YYYY-MM-DD
  std::vector<Label> labels;
  std::string created_at;  // "YYYY-MM-DD HH:MM:SS" (MySQL DATETIME文字列のまま保持)
  std::string updated_at;
};

// リクエストボディから作る入力型(バリデーション前)
struct TaskInput {
  std::string name;
  std::optional<std::string> description;
  std::string status_raw;
  std::string finished_on;
  std::vector<int64_t> label_ids;
};

}  // namespace backend_cpp::domain
