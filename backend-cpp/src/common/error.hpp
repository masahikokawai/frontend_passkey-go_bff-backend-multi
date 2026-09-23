#pragma once

#include <expected>
#include <string>

namespace backend_cpp::common {

// Repository層より上のレイヤーは例外を一切見ない。sql::Error等の例外は
// Repository層でcatchしAppErrorへ変換する(境界を1箇所に固定する設計、README.md参照)
enum class AppErrorKind {
  kNotFound,
  kInvalidRequest,
  kInvalidId,
  kInvalidStatus,
  kInvalidFinishedOn,
  kValidationError,
  kDbError,
  kUnauthorized,
};

struct AppError {
  AppErrorKind kind;
  std::string message;  // kValidationErrorのときのみ意味を持つ
};

template <typename T>
using Result = std::expected<T, AppError>;

inline AppError NotFound() { return {AppErrorKind::kNotFound, ""}; }
inline AppError InvalidRequest() { return {AppErrorKind::kInvalidRequest, ""}; }
inline AppError InvalidId() { return {AppErrorKind::kInvalidId, ""}; }
inline AppError InvalidStatus() { return {AppErrorKind::kInvalidStatus, ""}; }
inline AppError InvalidFinishedOn() { return {AppErrorKind::kInvalidFinishedOn, ""}; }
inline AppError ValidationError(std::string message) {
  return {AppErrorKind::kValidationError, std::move(message)};
}
inline AppError DbError() { return {AppErrorKind::kDbError, ""}; }
inline AppError Unauthorized() { return {AppErrorKind::kUnauthorized, ""}; }

// (http_status, json body) を返す。JSON形状はbackend-rust/backend-js-expressのerror.jsと
// 1文字も変えていない(error.jsのRestError一覧参照)
int HttpStatusFor(const AppError& err);
std::string JsonBodyFor(const AppError& err);

}  // namespace backend_cpp::common
