package com.bffgin.backend

// backend/internal/service/errors.go + internal/handler/v1/render.go のエラー分類をそのまま再現する
// (REST/gRPC双方のエラーマッピングがこの一つの型から導出されるようにする)
sealed abstract class AppError(val message: String) extends Exception(message)
object AppError {
  case object NotFound extends AppError("対象が見つかりません")
  final case class Validation(detail: String) extends AppError(s"入力値が不正です: $detail")
  case object UserNotProvisioned extends AppError("user_not_provisioned")
  case object Unauthorized extends AppError("unauthorized")
  case object InvalidToken extends AppError("invalid_token")
  // CONTRACT.mdセクション11: 外部公開API専用
  // azp クレームが external-api-client と不一致のとき
  case object ClientNotAllowed extends AppError("client_not_allowed")
  final case class ExternalUserIDRequired(detail: String) extends AppError(detail)
  final case class ExternalInvalidUserID(detail: String) extends AppError(detail)
  final case class ExternalInvalidCursor(detail: String) extends AppError(detail)
  final case class InvalidRequest(detail: String) extends AppError(detail)
  final case class InvalidStatus(raw: String) extends AppError(s"invalid_status: $raw")
  final case class InvalidFinishedOn(raw: String) extends AppError(s"invalid_finished_on: $raw")

  // backend/internal/handler/v1/render.go の renderServiceError と同じマッピングを純粋関数として持つ
  // (http4sのResponse 構築から切り離すことで、HTTPサーバを起動せずステータスコードのみテストできる)
  // 戻り値は (HTTPステータスコード, errorコード, 任意のmessage)
  def toRestStatus(e: Throwable): (Int, String, Option[String]) = e match {
    case NotFound               => (404, "not_found", None)
    case Validation(detail)     => (422, "validation_error", Some(s"入力値が不正です: $detail"))
    case UserNotProvisioned     => (403, "user_not_provisioned", None)
    case Unauthorized           => (401, "unauthorized", None)
    case InvalidToken           => (401, "invalid_token", None)
    case InvalidRequest(_)      => (400, "invalid_request", None)
    case InvalidStatus(_)       => (422, "invalid_status", None)
    case InvalidFinishedOn(_)   => (422, "invalid_finished_on", None)
    case ClientNotAllowed       => (403, "client_not_allowed", None)
    // Go実装(handler/external/task.go)はこの2つを "error" キーへ直接文字列を入れるだけで別途 message キーは持たない
    // 厳密に文字列一致させる必要があるためcodeへそのまま入れる
    case ExternalUserIDRequired(_) => (400, "user_id is required", None)
    case ExternalInvalidUserID(_)  => (400, "invalid user_id", None)
    case ExternalInvalidCursor(detail) => (422, detail, None)
    case _                      => (500, "internal_server_error", None)
  }
}
