#include "domain/task.hpp"

namespace backend_cpp::domain {

std::optional<TaskStatus> StatusFromString(const std::string& s) {
  if (s == "waiting") return TaskStatus::kWaiting;
  if (s == "work_in_progress") return TaskStatus::kWorkInProgress;
  if (s == "completed") return TaskStatus::kCompleted;
  return std::nullopt;
}

std::string StatusToString(TaskStatus status) {
  switch (status) {
    case TaskStatus::kWaiting:
      return "waiting";
    case TaskStatus::kWorkInProgress:
      return "work_in_progress";
    case TaskStatus::kCompleted:
      return "completed";
  }
  return "";
}

}  // namespace backend_cpp::domain
