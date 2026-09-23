#pragma once

#include <string>

#include "auth/jwt.hpp"
#include "common/error.hpp"
#include "repository/task_repository.hpp"

namespace backend_cpp::application {

// Authorizationヘッダの値("Bearer xxx")を検証し、user_idを解決する
// (backend(Go)のresolve_user_id、backend-rustのsrc/auth/mod.rsと同じ設計)
// REST/gRPCの両方から呼ばれる共通ロジック(認証を複製しない、README.md参照)
// ブロッキング呼び出しである点に注意: RESTはRunBlockingでdb_thread_poolへディスパッチして
// 呼ぶ、gRPCはCallback API自身のスレッドで直接呼ぶ(README.md「gRPCのスレッドモデル」節参照)
common::Result<int64_t> ResolveUserIdFromAuthHeader(const std::string& authorization_header,
                                                     repository::TaskRepository& repo,
                                                     auth::Dispatcher& dispatcher);

}  // namespace backend_cpp::application
