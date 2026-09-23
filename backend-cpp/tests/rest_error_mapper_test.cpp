#include "common/error.hpp"

#include <gtest/gtest.h>
#include <nlohmann/json.hpp>

// REST層のエラーマッピング(src/common/error.cpp: HttpStatusFor/JsonBodyFor)の単体テスト。
// backend-javaのRestErrorMapperTest・backend-rustのerror.rsのテストスイートと同じ観点
// (CONTRACT.mdセクション20.5のワイヤー契約パリティ、JSON形状・HTTPステータスを1文字も
// 変えていないことを確認する)。実サーバー・DB不要(AppErrorを渡すだけの純粋関数)
namespace backend_cpp::common {
namespace {

using json = nlohmann::json;

TEST(RestErrorMapper, UnauthorizedMapsTo401) {
  auto err = Unauthorized();
  EXPECT_EQ(HttpStatusFor(err), 401);
  EXPECT_EQ(json::parse(JsonBodyFor(err))["error"], "unauthenticated");
}

TEST(RestErrorMapper, InvalidRequestMapsTo400) {
  auto err = InvalidRequest();
  EXPECT_EQ(HttpStatusFor(err), 400);
  EXPECT_EQ(json::parse(JsonBodyFor(err))["error"], "invalid_request");
}

TEST(RestErrorMapper, InvalidIdMapsTo400) {
  auto err = InvalidId();
  EXPECT_EQ(HttpStatusFor(err), 400);
  EXPECT_EQ(json::parse(JsonBodyFor(err))["error"], "invalid_id");
}

TEST(RestErrorMapper, InvalidStatusMapsTo422) {
  auto err = InvalidStatus();
  EXPECT_EQ(HttpStatusFor(err), 422);
  EXPECT_EQ(json::parse(JsonBodyFor(err))["error"], "invalid_status");
}

TEST(RestErrorMapper, InvalidFinishedOnMapsTo422) {
  auto err = InvalidFinishedOn();
  EXPECT_EQ(HttpStatusFor(err), 422);
  EXPECT_EQ(json::parse(JsonBodyFor(err))["error"], "invalid_finished_on");
}

TEST(RestErrorMapper, ValidationMapsTo422WithMessage) {
  auto err = ValidationError("nameは20文字以内である必要があります");
  EXPECT_EQ(HttpStatusFor(err), 422);
  auto body = json::parse(JsonBodyFor(err));
  EXPECT_EQ(body["error"], "validation_error");
  EXPECT_EQ(body["message"], "nameは20文字以内である必要があります");
}

TEST(RestErrorMapper, NotFoundMapsTo404) {
  auto err = NotFound();
  EXPECT_EQ(HttpStatusFor(err), 404);
  EXPECT_EQ(json::parse(JsonBodyFor(err))["error"], "not_found");
}

TEST(RestErrorMapper, DbErrorMapsTo500AsInternalServerError) {
  auto err = DbError();
  EXPECT_EQ(HttpStatusFor(err), 500);
  EXPECT_EQ(json::parse(JsonBodyFor(err))["error"], "internal_server_error");
}

TEST(RestErrorMapper, ValidationErrorBodyDoesNotLeakIntoOtherKinds) {
  // messageフィールドはkValidationErrorのときのみ現れる(他の種類には含まれない)ことを確認する
  auto err = NotFound();
  auto body = json::parse(JsonBodyFor(err));
  EXPECT_FALSE(body.contains("message"));
}

}  // namespace
}  // namespace backend_cpp::common
