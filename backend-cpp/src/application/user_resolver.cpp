#include "application/user_resolver.hpp"

namespace backend_cpp::application {

common::Result<int64_t> ResolveUserIdFromAuthHeader(const std::string& authorization_header,
                                                     repository::TaskRepository& repo,
                                                     auth::Dispatcher& dispatcher) {
  auto claims = dispatcher.Verify(authorization_header);
  if (!claims) return std::unexpected(common::Unauthorized());

  std::optional<int64_t> user_id;
  if (auth::IsLocalIssuer(claims->iss)) {
    try {
      user_id = repo.FindUserById(std::stoll(claims->sub));
    } catch (...) {
      user_id = std::nullopt;
    }
  } else {
    user_id = repo.FindUserIdByKeycloakSub(claims->sub);
  }
  if (!user_id.has_value()) return std::unexpected(common::Unauthorized());
  return *user_id;
}

}  // namespace backend_cpp::application
