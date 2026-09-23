#include "external/external_handler.hpp"

#include <gtest/gtest.h>

// 外部公開API(src/external/external_handler.cpp)のクエリパース処理の単体テスト。
// 実サーバー・DB不要(文字列を渡すだけの純粋関数)。backend-c/tests/external_query_test.cと
// 同じ観点(user_id必須パラメータ、offset/cursorページングのデフォルト・clamp・不正値)
namespace backend_cpp::external {
namespace {

TEST(ParseUserId, RequiredWhenMissing) {
  auto q = ParseQueryParams("?page=1");
  int64_t user_id = 0;
  EXPECT_EQ(ParseUserId(q, user_id), UserIdParseResult::kRequired);
}

TEST(ParseUserId, RequiredWhenEmpty) {
  auto q = ParseQueryParams("?user_id=&page=1");
  int64_t user_id = 0;
  EXPECT_EQ(ParseUserId(q, user_id), UserIdParseResult::kRequired);
}

TEST(ParseUserId, InvalidWhenNotNumeric) {
  auto q = ParseQueryParams("?user_id=abc");
  int64_t user_id = 0;
  EXPECT_EQ(ParseUserId(q, user_id), UserIdParseResult::kInvalid);
}

TEST(ParseUserId, Ok) {
  auto q = ParseQueryParams("?user_id=42&page=2");
  int64_t user_id = 0;
  EXPECT_EQ(ParseUserId(q, user_id), UserIdParseResult::kOk);
  EXPECT_EQ(user_id, 42);
}

TEST(ParseOffsetPaging, Defaults) {
  auto q = ParseQueryParams("");
  int page = 0, page_size = 0;
  ParseOffsetPaging(q, page, page_size);
  EXPECT_EQ(page, 1);
  EXPECT_EQ(page_size, 10);
}

TEST(ParseOffsetPaging, ClampsBelowOne) {
  auto q = ParseQueryParams("?page=0&page_size=-5");
  int page = 0, page_size = 0;
  ParseOffsetPaging(q, page, page_size);
  EXPECT_EQ(page, 1);
  EXPECT_EQ(page_size, 1);
}

TEST(ParseOffsetPaging, ReadsExplicitValues) {
  auto q = ParseQueryParams("?page=3&page_size=25");
  int page = 0, page_size = 0;
  ParseOffsetPaging(q, page, page_size);
  EXPECT_EQ(page, 3);
  EXPECT_EQ(page_size, 25);
}

TEST(ParseOffsetPaging, IgnoresNonNumericPageAndFallsBackToDefault) {
  // std::stoiが例外を投げ、その値は変更されない(=デフォルトのまま)ことを確認する
  auto q = ParseQueryParams("?page=not-a-number&page_size=abc");
  int page = 0, page_size = 0;
  ParseOffsetPaging(q, page, page_size);
  EXPECT_EQ(page, 1);
  EXPECT_EQ(page_size, 10);
}

TEST(ParseCursorPaging, DefaultsToStart) {
  auto q = ParseQueryParams("");
  std::optional<int64_t> after_id = -1;
  int limit = 0;
  ParseCursorPaging(q, after_id, limit);
  EXPECT_FALSE(after_id.has_value());
  EXPECT_EQ(limit, 10);
}

TEST(ParseCursorPaging, ReadsCursorAndLimit) {
  auto q = ParseQueryParams("?cursor=123&limit=5");
  std::optional<int64_t> after_id;
  int limit = 0;
  ParseCursorPaging(q, after_id, limit);
  ASSERT_TRUE(after_id.has_value());
  EXPECT_EQ(*after_id, 123);
  EXPECT_EQ(limit, 5);
}

TEST(ParseCursorPaging, IgnoresGarbageCursor) {
  auto q = ParseQueryParams("?cursor=not-a-number&limit=5");
  std::optional<int64_t> after_id = 999;
  int limit = 0;
  ParseCursorPaging(q, after_id, limit);
  EXPECT_FALSE(after_id.has_value());
  EXPECT_EQ(limit, 5);
}

TEST(ParseCursorPaging, IgnoresEmptyCursor) {
  auto q = ParseQueryParams("?cursor=&limit=5");
  std::optional<int64_t> after_id = 999;
  int limit = 0;
  ParseCursorPaging(q, after_id, limit);
  EXPECT_FALSE(after_id.has_value());
}

TEST(ParseCursorPaging, ClampsLimitBelowOne) {
  auto q = ParseQueryParams("?limit=0");
  std::optional<int64_t> after_id;
  int limit = 0;
  ParseCursorPaging(q, after_id, limit);
  EXPECT_EQ(limit, 1);
}

TEST(UseCursorPaging, ReturnsTrueOnlyForOn) {
  EXPECT_TRUE(UseCursorPaging("on"));
  EXPECT_FALSE(UseCursorPaging("off"));
}

TEST(UseCursorPaging, ReturnsFalseForEmptyVariation) {
  EXPECT_FALSE(UseCursorPaging(""));
}

TEST(ParseQueryParams, HandlesMultipleParamsAndNoQueryString) {
  auto empty = ParseQueryParams("/external/v1/tasks");
  EXPECT_TRUE(empty.empty());

  auto multi = ParseQueryParams("/external/v1/tasks?user_id=1&page=2&page_size=3");
  EXPECT_EQ(multi.at("user_id"), "1");
  EXPECT_EQ(multi.at("page"), "2");
  EXPECT_EQ(multi.at("page_size"), "3");
}

}  // namespace
}  // namespace backend_cpp::external
