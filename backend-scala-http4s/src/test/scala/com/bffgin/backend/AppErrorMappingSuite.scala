package com.bffgin.backend

// backend/internal/handler/v1/render.go の renderServiceError と同じマッピングであることを確認する
// (CONTRACT.mdセクション5.1・20.5のワイヤー契約パリティ)
class AppErrorMappingSuite extends munit.FunSuite {
  test("NotFound -> 404 not_found") {
    assertEquals(AppError.toRestStatus(AppError.NotFound), (404, "not_found", None))
  }
  test("Validation -> 422 validation_error(message付き)") {
    val (code, key, msg) = AppError.toRestStatus(AppError.Validation("nameは必須です"))
    assertEquals(code, 422)
    assertEquals(key, "validation_error")
    assert(msg.isDefined)
  }
  test("UserNotProvisioned -> 403 user_not_provisioned") {
    assertEquals(AppError.toRestStatus(AppError.UserNotProvisioned), (403, "user_not_provisioned", None))
  }
  test("Unauthorized -> 401 unauthorized") {
    assertEquals(AppError.toRestStatus(AppError.Unauthorized), (401, "unauthorized", None))
  }
  test("InvalidToken -> 401 invalid_token") {
    assertEquals(AppError.toRestStatus(AppError.InvalidToken), (401, "invalid_token", None))
  }
  test("InvalidRequest -> 400 invalid_request") {
    val (code, key, _) = AppError.toRestStatus(AppError.InvalidRequest("x"))
    assertEquals(code, 400)
    assertEquals(key, "invalid_request")
  }
  test("InvalidStatus -> 422 invalid_status") {
    val (code, key, _) = AppError.toRestStatus(AppError.InvalidStatus("bogus"))
    assertEquals(code, 422)
    assertEquals(key, "invalid_status")
  }
  test("InvalidFinishedOn -> 422 invalid_finished_on") {
    val (code, key, _) = AppError.toRestStatus(AppError.InvalidFinishedOn("bogus"))
    assertEquals(code, 422)
    assertEquals(key, "invalid_finished_on")
  }
  test("未分類の例外 -> 500 internal_server_error") {
    val (code, key, _) = AppError.toRestStatus(new RuntimeException("boom"))
    assertEquals(code, 500)
    assertEquals(key, "internal_server_error")
  }

  // CONTRACT.mdセクション11: 外部公開API専用のエラー。Goのhandler/external/task.goは
  // これらをmessageキー無しで"error"に直接文字列を入れるだけなので、厳密に一致させる
  test("ClientNotAllowed -> 403 client_not_allowed") {
    assertEquals(AppError.toRestStatus(AppError.ClientNotAllowed), (403, "client_not_allowed", None))
  }
  test("ExternalUserIDRequired -> 400 \"user_id is required\"(Goと文字列完全一致)") {
    val (code, key, _) = AppError.toRestStatus(AppError.ExternalUserIDRequired("x"))
    assertEquals(code, 400)
    assertEquals(key, "user_id is required")
  }
  test("ExternalInvalidUserID -> 400 \"invalid user_id\"(Goと文字列完全一致)") {
    val (code, key, _) = AppError.toRestStatus(AppError.ExternalInvalidUserID("x"))
    assertEquals(code, 400)
    assertEquals(key, "invalid user_id")
  }
  test("ExternalInvalidCursor -> 422") {
    val (code, _, _) = AppError.toRestStatus(AppError.ExternalInvalidCursor("cursorの形式が不正です"))
    assertEquals(code, 422)
  }
}
