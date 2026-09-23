#include <gtest/gtest.h>

#include "common/time.hpp"
#include "domain/task.hpp"

using namespace backend_cpp;

TEST(Status, RoundTrip) {
  EXPECT_EQ(domain::StatusToString(domain::TaskStatus::kWaiting), "waiting");
  EXPECT_EQ(domain::StatusToString(domain::TaskStatus::kWorkInProgress), "work_in_progress");
  EXPECT_EQ(domain::StatusToString(domain::TaskStatus::kCompleted), "completed");
  EXPECT_EQ(domain::StatusFromString("waiting"), domain::TaskStatus::kWaiting);
  EXPECT_FALSE(domain::StatusFromString("bogus").has_value());
}

TEST(IsValidIsoDate, RejectsCalendarInvalidDate) {
  EXPECT_TRUE(common::IsValidIsoDate("2026-02-28"));
  EXPECT_FALSE(common::IsValidIsoDate("2026-02-30"));  // カレンダー上存在しない
  EXPECT_TRUE(common::IsValidIsoDate("2024-02-29"));   // うるう年
  EXPECT_FALSE(common::IsValidIsoDate("2026-13-01"));
  EXPECT_FALSE(common::IsValidIsoDate("not-a-date"));
}

TEST(Utf8CodepointLength, CountsCodepointsNotBytes) {
  EXPECT_EQ(common::Utf8CodepointLength("hello"), 5u);
  // "あ"は3バイトのUTF-8だが1コードポイント
  EXPECT_EQ(common::Utf8CodepointLength("あいう"), 3u);
  // 絵文字(サロゲートペア相当、4バイトUTF-8)も1コードポイントとして数える
  EXPECT_EQ(common::Utf8CodepointLength("\xF0\x9F\x98\x80"), 1u);
}

TEST(TodayUtcIso, ReturnsIsoDateFormat) {
  auto today = common::TodayUtcIso();
  EXPECT_EQ(today.size(), 10u);
  EXPECT_TRUE(common::IsValidIsoDate(today));
}
