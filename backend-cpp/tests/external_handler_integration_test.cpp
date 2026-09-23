#include "external/external_handler.hpp"

#include <gtest/gtest.h>
#include <mysql/mysql.h>

#include <boost/asio/co_spawn.hpp>
#include <boost/asio/detached.hpp>
#include <boost/asio/io_context.hpp>
#include <boost/asio/ip/tcp.hpp>
#include <memory>
#include <nlohmann/json.hpp>
#include <stdexcept>
#include <thread>

#include "auth/jwt.hpp"
#include "config.hpp"
#include "db/connection_pool.hpp"
#include "db_fixture.hpp"
#include "flags/feature_flag_poller.hpp"
#include "http/router.hpp"
#include "http/session.hpp"
#include "http_test_client.hpp"
#include "mock_jwks_server.hpp"
#include "repository/task_repository.hpp"
#include "test_token_helper.hpp"

// 外部公開API(CONTRACT.mdセクション11)の結合テスト。main.cppと同じ構成
// (実ExternalHandler+実Dispatcher+実FeatureFlagPoller)をテスト用ポートで起動し、
// 実際にHTTPでリクエストを送る(backend-c/tests/external_handler_integration_test.cの
// C++版)
namespace backend_cpp::external {
namespace {

namespace asio = boost::asio;
using asio::ip::tcp;
using json = nlohmann::json;
using backend_cpp::testutil::ExtractRsaPublicComponents;
using backend_cpp::testutil::GenerateRsaKeypair;
using backend_cpp::testutil::HttpGetWithHeaders;
using backend_cpp::testutil::MakeHmacToken;
using backend_cpp::testutil::MakeRsaToken;
using backend_cpp::testutil::MockJwksServer;
using backend_cpp::testutil::TestUser;

constexpr const char* kMockKeycloakIssuer = "http://mock-keycloak.test/realms/training";
constexpr const char* kAudience = "backend";
constexpr const char* kExternalApiClientId = "external-api-client";
constexpr const char* kHmacSecret = "test-secret";
constexpr unsigned short kJwksPort = 18544;
constexpr unsigned short kExternalPort = 18110;
constexpr const char* kFlagKey = "backend.external-tasks-pagination-v2";

// テスト対象のflagは全言語のテストスイートが共有するグローバルなDB状態のため、
// 元の値を読み取っておき、テスト終了時に必ず復元する(README.md「結合テスト」節参照)
class FeatureFlagOverride {
 public:
  FeatureFlagOverride(db::ConnectionPool& pool, const std::string& flag_key, bool enabled,
                       const std::string& variation)
      : pool_(pool), flag_key_(flag_key) {
    auto lease = pool_.Acquire();
    MYSQL* conn = lease.get();
    std::string sql =
        "SELECT enabled, default_variation FROM feature_flags WHERE flag_key = '" + flag_key_ + "'";
    if (mysql_query(conn, sql.c_str()) != 0) throw std::runtime_error(mysql_error(conn));
    MYSQL_RES* res = mysql_store_result(conn);
    if (res == nullptr) throw std::runtime_error("feature_flags row not found");
    MYSQL_ROW row = mysql_fetch_row(res);
    if (row == nullptr) {
      mysql_free_result(res);
      throw std::runtime_error("feature_flags row not found: " + flag_key_);
    }
    original_enabled_ = std::string(row[0]) == "1";
    original_variation_ = row[1] ? row[1] : "";
    mysql_free_result(res);

    Set(conn, enabled, variation);
  }

  ~FeatureFlagOverride() {
    try {
      auto lease = pool_.Acquire();
      Set(lease.get(), original_enabled_, original_variation_);
    } catch (...) {
      // 復元に失敗してもテスト自体は落とさない。ただし他言語のテストに影響しうるため
      // 実運用ではここのログを必ず確認すること
    }
  }

 private:
  void Set(MYSQL* conn, bool enabled, const std::string& variation) {
    std::string sql = "UPDATE feature_flags SET enabled = " + std::string(enabled ? "1" : "0") +
                       ", default_variation = '" + variation + "' WHERE flag_key = '" + flag_key_ +
                       "'";
    if (mysql_query(conn, sql.c_str()) != 0) throw std::runtime_error(mysql_error(conn));
  }

  db::ConnectionPool& pool_;
  std::string flag_key_;
  bool original_enabled_ = false;
  std::string original_variation_;
};

class ExternalHandlerIntegrationTest : public ::testing::Test {
 protected:
  static void SetUpTestSuite() {
    config_ = new Config(Config::FromEnv());
    pool_ = new db::ConnectionPool(*config_);
    repo_ = new repository::TaskRepository(*pool_);
    db_pool_ = new asio::thread_pool(4);

    rsa_key_ = GenerateRsaKeypair();
    auto ne = ExtractRsaPublicComponents(rsa_key_);
    json jwks{{"keys",
               json::array({json{{"kty", "RSA"},
                                  {"kid", "mock-kid"},
                                  {"use", "sig"},
                                  {"n", ne.n_b64url},
                                  {"e", ne.e_b64url}}})}};
    jwks_server_ = new MockJwksServer(kJwksPort, jwks.dump());

    dispatcher_ = new auth::Dispatcher();
    dispatcher_->Register(kMockKeycloakIssuer,
                           std::make_shared<auth::JwksVerifier>(jwks_server_->Url(),
                                                                  kMockKeycloakIssuer, kAudience));
    dispatcher_->Register(auth::kLocalHmacIssuer, std::make_shared<auth::HmacVerifier>(
                                                       kHmacSecret, auth::kLocalHmacIssuer, kAudience));

    flag_poller_ = new flags::FeatureFlagPoller(*config_);
    flag_poller_->Start();

    handler_ = new ExternalHandler(*repo_, *db_pool_, *dispatcher_, *flag_poller_,
                                    kExternalApiClientId);
    router_ = new http::Router();
    router_->Add("GET", "/external/v1/tasks", false,
                 [](const http::HttpRequest& req, int64_t id) { return handler_->List(req, id); });

    io_ctx_ = new asio::io_context();
    acceptor_ = new tcp::acceptor(*io_ctx_, {tcp::v4(), kExternalPort});
    asio::co_spawn(*io_ctx_, http::RunListener(*acceptor_, *router_), asio::detached);
    io_thread_ = new std::thread([] { io_ctx_->run(); });
  }

  static void TearDownTestSuite() {
    io_ctx_->stop();
    io_thread_->join();
    delete io_thread_;
    delete acceptor_;
    delete io_ctx_;
    delete router_;
    delete handler_;
    flag_poller_->Stop();
    delete flag_poller_;
    delete dispatcher_;
    delete jwks_server_;
    EVP_PKEY_free(rsa_key_);
    db_pool_->join();
    delete db_pool_;
    delete repo_;
    delete pool_;
    delete config_;
  }

  static std::string ValidKeycloakToken(const std::string& sub) {
    return MakeRsaToken(rsa_key_, "mock-kid", kMockKeycloakIssuer, kAudience, sub, 3600,
                         kExternalApiClientId);
  }

  static backend_cpp::testutil::HttpTestResponse Get(const std::string& query,
                                                      const std::string& bearer = "") {
    std::map<std::string, std::string> headers;
    if (!bearer.empty()) headers["Authorization"] = "Bearer " + bearer;
    return HttpGetWithHeaders("127.0.0.1", kExternalPort, "/external/v1/tasks?" + query, headers);
  }

  static Config* config_;
  static db::ConnectionPool* pool_;
  static repository::TaskRepository* repo_;
  static asio::thread_pool* db_pool_;
  static EVP_PKEY* rsa_key_;
  static MockJwksServer* jwks_server_;
  static auth::Dispatcher* dispatcher_;
  static flags::FeatureFlagPoller* flag_poller_;
  static ExternalHandler* handler_;
  static http::Router* router_;
  static asio::io_context* io_ctx_;
  static tcp::acceptor* acceptor_;
  static std::thread* io_thread_;
};

Config* ExternalHandlerIntegrationTest::config_ = nullptr;
db::ConnectionPool* ExternalHandlerIntegrationTest::pool_ = nullptr;
repository::TaskRepository* ExternalHandlerIntegrationTest::repo_ = nullptr;
asio::thread_pool* ExternalHandlerIntegrationTest::db_pool_ = nullptr;
EVP_PKEY* ExternalHandlerIntegrationTest::rsa_key_ = nullptr;
MockJwksServer* ExternalHandlerIntegrationTest::jwks_server_ = nullptr;
auth::Dispatcher* ExternalHandlerIntegrationTest::dispatcher_ = nullptr;
flags::FeatureFlagPoller* ExternalHandlerIntegrationTest::flag_poller_ = nullptr;
ExternalHandler* ExternalHandlerIntegrationTest::handler_ = nullptr;
http::Router* ExternalHandlerIntegrationTest::router_ = nullptr;
asio::io_context* ExternalHandlerIntegrationTest::io_ctx_ = nullptr;
tcp::acceptor* ExternalHandlerIntegrationTest::acceptor_ = nullptr;
std::thread* ExternalHandlerIntegrationTest::io_thread_ = nullptr;

TEST_F(ExternalHandlerIntegrationTest, RejectsMissingAuthorization) {
  auto res = Get("user_id=1");
  EXPECT_EQ(res.status, 401);
}

TEST_F(ExternalHandlerIntegrationTest, RejectsWrongAzp) {
  auto token = MakeRsaToken(rsa_key_, "mock-kid", kMockKeycloakIssuer, kAudience, "1", 3600,
                             "someone-else-client");
  auto res = Get("user_id=1", token);
  EXPECT_EQ(res.status, 401);
}

TEST_F(ExternalHandlerIntegrationTest, RejectsLocalHmacIssuerEvenWithCorrectAzp) {
  // ローカルHMAC発行のJWTは署名検証自体は正しく通っても、外部公開APIでは受け付けない
  // (Keycloak発行のみ許可、CONTRACT.mdセクション11)
  auto token =
      MakeHmacToken(kHmacSecret, auth::kLocalHmacIssuer, kAudience, "1", 3600, kExternalApiClientId);
  auto res = Get("user_id=1", token);
  EXPECT_EQ(res.status, 401);
}

TEST_F(ExternalHandlerIntegrationTest, RejectsMissingUserId) {
  auto token = ValidKeycloakToken("1");
  auto res = Get("page=1", token);
  EXPECT_EQ(res.status, 400);
  EXPECT_NE(res.body.find("user_id_required"), std::string::npos);
}

TEST_F(ExternalHandlerIntegrationTest, OffsetPaginationAcrossPages) {
  TestUser user(*pool_);
  for (int i = 0; i < 3; ++i) {
    domain::TaskInput input;
    input.name = "cpp-ext-it-" + std::to_string(i);
    input.status_raw = "waiting";
    input.finished_on = "2099-01-01";
    auto created = repo_->Create(user.id(), input, domain::TaskStatus::kWaiting);
    ASSERT_TRUE(created.has_value());
  }

  auto token = ValidKeycloakToken(std::to_string(user.id()));
  auto page1 = Get("user_id=" + std::to_string(user.id()) + "&page=1&page_size=2", token);
  ASSERT_EQ(page1.status, 200);
  auto page1_json = json::parse(page1.body);
  EXPECT_EQ(page1_json["tasks"].size(), 2u);
  EXPECT_EQ(page1_json["total"], 3);
  EXPECT_EQ(page1_json["page"], 1);

  auto page2 = Get("user_id=" + std::to_string(user.id()) + "&page=2&page_size=2", token);
  ASSERT_EQ(page2.status, 200);
  auto page2_json = json::parse(page2.body);
  EXPECT_EQ(page2_json["tasks"].size(), 1u);

  // 外部APIのレスポンスにはuser_idを含めない(内部CRUDと異なる形状、README.md参照)
  ASSERT_FALSE(page1_json["tasks"].empty());
  EXPECT_FALSE(page1_json["tasks"][0].contains("user_id"));
}

TEST_F(ExternalHandlerIntegrationTest, CursorPaginationChainsToNull) {
  FeatureFlagOverride flag_override(*pool_, kFlagKey, true, "on");
  // ポーラーの次回ポーリング(最大10秒)を待たず、確実に反映させるため軽くリトライする
  auto token = ValidKeycloakToken("999999");  // user_idはtasksが無くてもcursor形状の確認はできる

  TestUser user(*pool_);
  for (int i = 0; i < 2; ++i) {
    domain::TaskInput input;
    input.name = "cpp-ext-it-cursor-" + std::to_string(i);
    input.status_raw = "waiting";
    input.finished_on = "2099-01-01";
    auto created = repo_->Create(user.id(), input, domain::TaskStatus::kWaiting);
    ASSERT_TRUE(created.has_value());
  }
  token = ValidKeycloakToken(std::to_string(user.id()));

  json last;
  bool saw_v2_shape = false;
  for (int attempt = 0; attempt < 150 && !saw_v2_shape; ++attempt) {
    auto res = Get("user_id=" + std::to_string(user.id()) + "&limit=1", token);
    if (res.status != 200) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
      continue;
    }
    last = json::parse(res.body);
    if (last.contains("next_cursor")) {
      saw_v2_shape = true;
      break;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
  }
  ASSERT_TRUE(saw_v2_shape) << "flag_pollerがbackend.external-tasks-pagination-v2をONと"
                               "認識するまでにタイムアウトした";
  EXPECT_EQ(last["tasks"].size(), 1u);
  ASSERT_FALSE(last["next_cursor"].is_null());

  auto page2 = Get("user_id=" + std::to_string(user.id()) + "&limit=1&cursor=" +
                        last["next_cursor"].get<std::string>(),
                    token);
  ASSERT_EQ(page2.status, 200);
  auto page2_json = json::parse(page2.body);
  EXPECT_EQ(page2_json["tasks"].size(), 1u);
  // 【この設計の既知の挙動】next_cursorは「1件でも返せば必ず最後の行のid」になる
  // (TaskRepository::ListCursorExternalのnext_cursor_out、次ページの有無を先読みしない
  // 簡略設計)。「次ページ無し」は、そのカーソルで再度呼んで0件が返って初めて分かる
  ASSERT_FALSE(page2_json["next_cursor"].is_null());

  auto page3 = Get("user_id=" + std::to_string(user.id()) + "&limit=1&cursor=" +
                        page2_json["next_cursor"].get<std::string>(),
                    token);
  ASSERT_EQ(page3.status, 200);
  auto page3_json = json::parse(page3.body);
  EXPECT_EQ(page3_json["tasks"].size(), 0u);
  EXPECT_TRUE(page3_json["next_cursor"].is_null());  // 次ページ無し
}

}  // namespace
}  // namespace backend_cpp::external
