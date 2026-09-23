#include "common/logging.hpp"

#include <iostream>

namespace backend_cpp::common {

namespace {
bool g_debug_enabled = false;
}  // namespace

void LogModuleInit(const std::string& log_level) { g_debug_enabled = (log_level == "debug"); }

void LogDebug(const std::string& line) {
  if (!g_debug_enabled) return;
  std::cout << line << std::endl;
}

void LogInfo(const std::string& line) { std::cout << line << std::endl; }

}  // namespace backend_cpp::common
