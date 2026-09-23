#include "common/error.hpp"

#include <nlohmann/json.hpp>

namespace backend_cpp::common {

int HttpStatusFor(const AppError& err) {
  switch (err.kind) {
    case AppErrorKind::kNotFound:
      return 404;
    case AppErrorKind::kInvalidRequest:
    case AppErrorKind::kInvalidId:
      return 400;
    case AppErrorKind::kInvalidStatus:
    case AppErrorKind::kInvalidFinishedOn:
    case AppErrorKind::kValidationError:
      return 422;
    case AppErrorKind::kDbError:
      return 500;
    case AppErrorKind::kUnauthorized:
      return 401;
  }
  return 500;
}

std::string JsonBodyFor(const AppError& err) {
  nlohmann::json j;
  switch (err.kind) {
    case AppErrorKind::kNotFound:
      j = {{"error", "not_found"}};
      break;
    case AppErrorKind::kInvalidRequest:
      j = {{"error", "invalid_request"}};
      break;
    case AppErrorKind::kInvalidId:
      j = {{"error", "invalid_id"}};
      break;
    case AppErrorKind::kInvalidStatus:
      j = {{"error", "invalid_status"}};
      break;
    case AppErrorKind::kInvalidFinishedOn:
      j = {{"error", "invalid_finished_on"}};
      break;
    case AppErrorKind::kValidationError:
      j = {{"error", "validation_error"}, {"message", err.message}};
      break;
    case AppErrorKind::kDbError:
      j = {{"error", "internal_server_error"}};
      break;
    case AppErrorKind::kUnauthorized:
      j = {{"error", "unauthenticated"}};
      break;
  }
  return j.dump();
}

}  // namespace backend_cpp::common
