#include "common/time.hpp"

#include <chrono>
#include <cstdio>
#include <ctime>

namespace backend_cpp::common {

std::string TodayUtcIso() {
  const auto now = std::chrono::system_clock::now();
  const std::time_t tt = std::chrono::system_clock::to_time_t(now);
  std::tm utc_tm{};
  gmtime_r(&tt, &utc_tm);  // gmtime_r: UTC基準(localtime_rは絶対に使わない)
  char buf[11];
  std::snprintf(buf, sizeof(buf), "%04d-%02d-%02d", utc_tm.tm_year + 1900, utc_tm.tm_mon + 1,
                utc_tm.tm_mday);
  return std::string(buf);
}

bool IsValidIsoDate(const std::string& s) {
  if (s.size() != 10 || s[4] != '-' || s[7] != '-') return false;
  for (int i : {0, 1, 2, 3, 5, 6, 8, 9}) {
    if (!std::isdigit(static_cast<unsigned char>(s[i]))) return false;
  }
  int year = std::stoi(s.substr(0, 4));
  int month = std::stoi(s.substr(5, 2));
  int day = std::stoi(s.substr(8, 2));
  if (month < 1 || month > 12 || day < 1) return false;
  static const int kDaysInMonth[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
  int max_day = kDaysInMonth[month - 1];
  bool leap = (year % 4 == 0 && year % 100 != 0) || (year % 400 == 0);
  if (month == 2 && leap) max_day = 29;
  return day <= max_day;
}

size_t Utf8CodepointLength(const std::string& s) {
  size_t count = 0;
  for (size_t i = 0; i < s.size();) {
    unsigned char c = s[i];
    if ((c & 0x80) == 0x00) {
      i += 1;
    } else if ((c & 0xE0) == 0xC0) {
      i += 2;
    } else if ((c & 0xF0) == 0xE0) {
      i += 3;
    } else if ((c & 0xF8) == 0xF0) {
      i += 4;
    } else {
      i += 1;  // 不正なバイト列はそれ以上進めない事故を避けるため1バイトずつ進める
    }
    ++count;
  }
  return count;
}

}  // namespace backend_cpp::common
