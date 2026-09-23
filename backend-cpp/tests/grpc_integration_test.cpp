#include <grpcpp/grpcpp.h>
#include <gtest/gtest.h>
#include <mysql/mysql.h>

#include <memory>
#include <string>
#include <thread>

#include "auth/jwt.hpp"
#include "config.hpp"
#include "db/connection_pool.hpp"
#include "db_fixture.hpp"
#include "grpc/task_grpc_service.hpp"
#include "repository/task_repository.hpp"
#include "task.grpc.pb.h"
#include "test_token_helper.hpp"

// gRPC v2の結合テスト。main.cppと同じ構成(実TaskGrpcServiceImpl+実Dispatcher)を
// テスト用ポートで起動し、生成されたクライアントスタブから実際にRPCを送る
// (backend-c/tests/task_grpc_integration_test.cのC++版、ただしgRPC C++は生成スタブが
// あるため自前のワイヤーフォーマット実装は不要)
namespace backend_cpp::grpcservice {
namespace {

using backend_cpp::testutil::MakeHmacToken;
using backend_cpp::testutil::TestLabel;
using backend_cpp::testutil::TestUser;

constexpr const char* kHmacSecret = "test-secret";
constexpr const char* kAudience = "backend";
constexpr const char* kListenAddr = "127.0.0.1:19099";

class GrpcIntegrationTest : public ::testing::Test {
 protected:
  static void SetUpTestSuite() {
    config_ = new Config(Config::FromEnv());
    pool_ = new db::ConnectionPool(*config_);
    repo_ = new repository::TaskRepository(*pool_);
    dispatcher_ = new auth::Dispatcher();
    dispatcher_->Register(auth::kLocalHmacIssuer, std::make_shared<auth::HmacVerifier>(
                                                       kHmacSecret, auth::kLocalHmacIssuer, kAudience));
    service_ = new TaskGrpcServiceImpl(*repo_, *dispatcher_);

    grpc::ServerBuilder builder;
    builder.AddListeningPort(kListenAddr, grpc::InsecureServerCredentials());
    builder.RegisterService(service_);
    server_ = builder.BuildAndStart();

    channel_ = grpc::CreateChannel(kListenAddr, grpc::InsecureChannelCredentials());
    stub_ = task::v1::TaskService::NewStub(channel_);
  }

  static void TearDownTestSuite() {
    server_->Shutdown();
    server_.reset();
    delete service_;
    delete dispatcher_;
    delete repo_;
    delete pool_;
    delete config_;
  }

  static std::string ValidToken(const std::string& sub) {
    return MakeHmacToken(kHmacSecret, auth::kLocalHmacIssuer, kAudience, sub, 3600);
  }

  static Config* config_;
  static db::ConnectionPool* pool_;
  static repository::TaskRepository* repo_;
  static auth::Dispatcher* dispatcher_;
  static TaskGrpcServiceImpl* service_;
  static std::unique_ptr<grpc::Server> server_;
  static std::shared_ptr<grpc::Channel> channel_;
  static std::unique_ptr<task::v1::TaskService::Stub> stub_;
};

Config* GrpcIntegrationTest::config_ = nullptr;
db::ConnectionPool* GrpcIntegrationTest::pool_ = nullptr;
repository::TaskRepository* GrpcIntegrationTest::repo_ = nullptr;
auth::Dispatcher* GrpcIntegrationTest::dispatcher_ = nullptr;
TaskGrpcServiceImpl* GrpcIntegrationTest::service_ = nullptr;
std::unique_ptr<grpc::Server> GrpcIntegrationTest::server_;
std::shared_ptr<grpc::Channel> GrpcIntegrationTest::channel_;
std::unique_ptr<task::v1::TaskService::Stub> GrpcIntegrationTest::stub_;

TEST_F(GrpcIntegrationTest, FullCrudRoundTrip) {
  TestUser user(*pool_);
  std::string token = ValidToken(std::to_string(user.id()));

  task::v1::CreateTaskRequest create_req;
  create_req.set_name("cpp-grpc-it-1");
  create_req.set_status("waiting");
  create_req.set_finished_on("2099-01-01");
  task::v1::Task created;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->CreateTask(&ctx, create_req, &created);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }
  EXPECT_EQ(created.name(), "cpp-grpc-it-1");

  task::v1::GetTaskRequest get_req;
  get_req.set_id(created.id());
  task::v1::Task fetched;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->GetTask(&ctx, get_req, &fetched);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }
  EXPECT_EQ(fetched.id(), created.id());

  task::v1::UpdateTaskRequest update_req;
  update_req.set_id(created.id());
  update_req.set_name("cpp-grpc-updated");  // 20コードポイント以内であること(name制約)
  update_req.set_status("completed");
  update_req.set_finished_on("2099-02-02");
  task::v1::Task updated;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->UpdateTask(&ctx, update_req, &updated);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }
  EXPECT_EQ(updated.name(), "cpp-grpc-updated");
  EXPECT_EQ(updated.status(), "completed");

  task::v1::DeleteTaskRequest delete_req;
  delete_req.set_id(created.id());
  task::v1::DeleteTaskResponse delete_resp;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->DeleteTask(&ctx, delete_req, &delete_resp);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }

  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    task::v1::Task after_delete;
    auto status = stub_->GetTask(&ctx, get_req, &after_delete);
    EXPECT_EQ(status.error_code(), grpc::StatusCode::NOT_FOUND);
  }
}

TEST_F(GrpcIntegrationTest, DeleteRemovesTaskLabelsRows) {
  TestUser user(*pool_);
  TestLabel label(*pool_);
  std::string token = ValidToken(std::to_string(user.id()));

  task::v1::CreateTaskRequest create_req;
  create_req.set_name("cpp-grpc-it-2");
  create_req.set_status("waiting");
  create_req.set_finished_on("2099-01-01");
  create_req.add_label_ids(static_cast<uint64_t>(label.id()));
  task::v1::Task created;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->CreateTask(&ctx, create_req, &created);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }
  ASSERT_EQ(created.labels_size(), 1);

  task::v1::DeleteTaskRequest delete_req;
  delete_req.set_id(created.id());
  task::v1::DeleteTaskResponse delete_resp;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->DeleteTask(&ctx, delete_req, &delete_resp);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }

  auto lease = pool_->Acquire();
  MYSQL* conn = lease.get();
  std::string sql =
      "SELECT COUNT(*) FROM task_labels WHERE task_id = " + std::to_string(created.id());
  ASSERT_EQ(mysql_query(conn, sql.c_str()), 0);
  MYSQL_RES* res = mysql_store_result(conn);
  ASSERT_NE(res, nullptr);
  MYSQL_ROW row = mysql_fetch_row(res);
  ASSERT_NE(row, nullptr);
  EXPECT_STREQ(row[0], "0");
  mysql_free_result(res);
}

TEST_F(GrpcIntegrationTest, UnauthenticatedCallRejected) {
  task::v1::ListTasksRequest req;
  task::v1::ListTasksResponse resp;
  grpc::ClientContext ctx;  // authorizationメタデータを付けない
  auto status = stub_->ListTasks(&ctx, req, &resp);
  EXPECT_EQ(status.error_code(), grpc::StatusCode::UNAUTHENTICATED);
}

TEST_F(GrpcIntegrationTest, ExpiredTokenRejected) {
  TestUser user(*pool_);
  std::string expired =
      MakeHmacToken(kHmacSecret, auth::kLocalHmacIssuer, kAudience, std::to_string(user.id()), -3600);

  task::v1::ListTasksRequest req;
  task::v1::ListTasksResponse resp;
  grpc::ClientContext ctx;
  ctx.AddMetadata("authorization", "Bearer " + expired);
  auto status = stub_->ListTasks(&ctx, req, &resp);
  EXPECT_EQ(status.error_code(), grpc::StatusCode::UNAUTHENTICATED);
}

TEST_F(GrpcIntegrationTest, ListTasksCursorPagination) {
  TestUser user(*pool_);
  std::string token = ValidToken(std::to_string(user.id()));
  std::vector<uint64_t> created_ids;

  for (int i = 0; i < 3; ++i) {
    task::v1::CreateTaskRequest create_req;
    create_req.set_name("cpp-grpc-it-cursor-" + std::to_string(i));
    create_req.set_status("waiting");
    create_req.set_finished_on("2099-01-01");
    task::v1::Task created;
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->CreateTask(&ctx, create_req, &created);
    ASSERT_TRUE(status.ok()) << status.error_message();
    created_ids.push_back(created.id());
  }

  task::v1::ListTasksRequest list_req;
  list_req.set_limit(2);
  task::v1::ListTasksResponse list_resp;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->ListTasks(&ctx, list_req, &list_resp);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }
  EXPECT_EQ(list_resp.tasks_size(), 2);
  ASSERT_NE(list_resp.next_cursor(), 0u);

  task::v1::ListTasksRequest page2_req;
  page2_req.set_limit(2);
  page2_req.set_cursor(list_resp.next_cursor());
  task::v1::ListTasksResponse page2_resp;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->ListTasks(&ctx, page2_req, &page2_resp);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }
  EXPECT_EQ(page2_resp.tasks_size(), 1);
  // 【この設計の既知の挙動】next_cursorは「1件でも返せば必ず最後の行のid」になる
  // (ListCursorExternalのnext_cursor_out、まだ次ページがあるかどうかの先読みはしない
  // 簡略設計、backend-cのtask_repository_list_cursorと同じ思想、README.md参照)。
  // そのため「次ページ無し」は、そのカーソルでもう一度呼んで0件が返ってきて
  // 初めて確認できる
  ASSERT_NE(page2_resp.next_cursor(), 0u);

  task::v1::ListTasksRequest page3_req;
  page3_req.set_limit(2);
  page3_req.set_cursor(page2_resp.next_cursor());
  task::v1::ListTasksResponse page3_resp;
  {
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    auto status = stub_->ListTasks(&ctx, page3_req, &page3_resp);
    ASSERT_TRUE(status.ok()) << status.error_message();
  }
  EXPECT_EQ(page3_resp.tasks_size(), 0);
  EXPECT_EQ(page3_resp.next_cursor(), 0u);  // 次ページ無し

  for (auto id : created_ids) {
    task::v1::DeleteTaskRequest del_req;
    del_req.set_id(id);
    task::v1::DeleteTaskResponse del_resp;
    grpc::ClientContext ctx;
    ctx.AddMetadata("authorization", "Bearer " + token);
    stub_->DeleteTask(&ctx, del_req, &del_resp);
  }
}

}  // namespace
}  // namespace backend_cpp::grpcservice
