#pragma once

#include <boost/asio/awaitable.hpp>
#include <boost/asio/thread_pool.hpp>
#include <cstdint>
#include <map>
#include <optional>
#include <string>

#include "auth/jwt.hpp"
#include "flags/feature_flag_poller.hpp"
#include "http/http_types.hpp"
#include "repository/task_repository.hpp"

namespace backend_cpp::external {

namespace asio = boost::asio;
using backend_cpp::http::HttpRequest;
using backend_cpp::http::HttpResponse;

// ---- テスト容易化のために抽出した純粋関数群 ----
// ExternalHandler::Listから呼ばれるのと全く同じロジックを、実サーバー・実DB無しで
// 直接呼び出せるようにする(backend-c/src/external/external_query.cと同じ観点、
// tests/external_query_test.cpp・tests/external_auth_test.cpp参照)。
// 本番コード(List())はここで宣言する関数をそのまま呼ぶだけで、挙動は変えていない

// targetの"?"より後ろのクエリ文字列をkey/value辞書へ分解する
std::map<std::string, std::string> ParseQueryParams(const std::string& target);

enum class UserIdParseResult { kOk, kRequired, kInvalid };
// user_id必須パラメータのパース。欠落/空文字はkRequired、非数値はkInvalid
UserIdParseResult ParseUserId(const std::map<std::string, std::string>& q, int64_t& user_id);

// page/page_size(offsetページング)。未指定はデフォルト(1, 10)、1未満はclampする
void ParseOffsetPaging(const std::map<std::string, std::string>& q, int& page, int& page_size);

// cursor/limit(cursorページング)。cursor未指定・非数値はafter_id=nulloptのまま
// (先頭からの意味)、limitは1未満をclampする
void ParseCursorPaging(const std::map<std::string, std::string>& q, std::optional<int64_t>& after_id,
                       int& limit);

// Feature Flag(backend.external-tasks-pagination-v2)のvariation文字列から
// cursorページングを使うかどうかを判定する
bool UseCursorPaging(const std::string& variation);

// Client Credentials Grant(Keycloak発行、azp一致)のみ受け付ける認証チェック。
// authorization_headerはAuthorizationヘッダの値そのもの("Bearer xxx")、
// ヘッダ自体が無ければstd::nulloptを渡す。失敗時は理由を問わずstd::nullopt
// (backend(Go)のRequireExternalClientAuth、backend-javaのExternalAuth.requireExternalClientと
// 同じ「missing header/unknown issuer/local issuer/azp不一致を区別せず401」という設計)
std::optional<auth::Claims> RequireExternalClientAuth(auth::Dispatcher& dispatcher,
                                                       const std::optional<std::string>& authorization_header,
                                                       const std::string& external_api_client_id);

// CONTRACT.mdセクション11: bffを経由しない外部公開API
// Client Credentials Grant(Keycloak発行、azp=EXTERNAL_API_CLIENT_IDのみ受け付ける)
class ExternalHandler {
 public:
  ExternalHandler(repository::TaskRepository& repo, asio::thread_pool& db_pool,
                   auth::Dispatcher& dispatcher, flags::FeatureFlagPoller& flags,
                   std::string external_api_client_id)
      : repo_(repo),
        db_pool_(db_pool),
        dispatcher_(dispatcher),
        flags_(flags),
        external_api_client_id_(std::move(external_api_client_id)) {}

  asio::awaitable<HttpResponse> List(const HttpRequest& req, int64_t /*unused*/);

 private:
  repository::TaskRepository& repo_;
  asio::thread_pool& db_pool_;
  auth::Dispatcher& dispatcher_;
  flags::FeatureFlagPoller& flags_;
  std::string external_api_client_id_;
};

}  // namespace backend_cpp::external
