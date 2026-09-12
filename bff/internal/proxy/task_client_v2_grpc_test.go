package proxy

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
	taskv1 "github.com/masahikokawai/bff-gin/bff/proto/task/v1"
)

// backend v2(gRPC)がエラーを返した場合に、bffのTaskClientV2がそれを正しく
// 分類しているかを検証する(REST v1(task_client_v1_test.go)はhttptestで同様の
// エラー分類を確認しているが、v2(gRPC)側にはこれまでテストが1つも無かった)
//
// 特に Unauthenticated(codes.Unauthenticated)は auth.ErrUpstreamUnauthorized へ変換される必要がある
// task_route.goのdo()がこれを見てリアクティブリフレッシュを判断するため
// それ以外(InvalidArgument等)は一般エラーとして扱われ、リフレッシュは試みられないことを確認する
type fakeTaskServiceServer struct {
	taskv1.UnimplementedTaskServiceServer
	err error
}

func (s *fakeTaskServiceServer) ListTasks(context.Context, *taskv1.ListTasksRequest) (*taskv1.ListTasksResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &taskv1.ListTasksResponse{Tasks: nil, NextCursor: 0}, nil
}

func (s *fakeTaskServiceServer) CreateTask(context.Context, *taskv1.CreateTaskRequest) (*taskv1.Task, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &taskv1.Task{Id: 1}, nil
}

func (s *fakeTaskServiceServer) DeleteTask(context.Context, *taskv1.DeleteTaskRequest) (*taskv1.DeleteTaskResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &taskv1.DeleteTaskResponse{}, nil
}

// newTestTaskClientV2 は実際にTCPリスナーでgRPCサーバーを立て(bufconnではなく
// 本番同様のgrpc.NewClientを使う既存コードとの整合を優先)、TaskClientV2を返す
func newTestTaskClientV2(t *testing.T, fake *fakeTaskServiceServer) *TaskClientV2 {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := grpc.NewServer()
	taskv1.RegisterTaskServiceServer(server, fake)
	go func() {
		_ = server.Serve(lis)
	}()
	t.Cleanup(server.Stop)

	client, closeFn, err := NewTaskClientV2(lis.Addr().String())
	if err != nil {
		t.Fatalf("NewTaskClientV2() error = %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	return client
}

func TestTaskClientV2_List_UnauthenticatedError_ReturnsErrUpstreamUnauthorized(t *testing.T) {
	fake := &fakeTaskServiceServer{err: status.Error(codes.Unauthenticated, "invalid_token")}
	client := newTestTaskClientV2(t, fake)

	_, err := client.List(context.Background(), "bad-token", 1, TaskFilter{Limit: 20})
	if !errors.Is(err, auth.ErrUpstreamUnauthorized) {
		t.Errorf("err = %v, want auth.ErrUpstreamUnauthorized", err)
	}
}

func TestTaskClientV2_Create_InvalidArgumentError_ReturnsGenericError(t *testing.T) {
	fake := &fakeTaskServiceServer{err: status.Error(codes.InvalidArgument, "name is required")}
	client := newTestTaskClientV2(t, fake)

	_, err := client.Create(context.Background(), "token", 1, TaskInput{Name: ""})
	if err == nil {
		t.Fatal("err = nil, want error")
	}
	if errors.Is(err, auth.ErrUpstreamUnauthorized) {
		t.Errorf("err = %v, InvalidArgumentはErrUpstreamUnauthorizedへ分類されるべきではない(リフレッシュ対象は認証エラーのみ)", err)
	}
}

// 実機検証で発覚した不具合の回帰テスト(REST v1版はtask_client_v1_test.go参照):
// admin 画面でログイン中のユーザーを削除すると、backendはcodes.PermissionDenied("user not provisioned")を返す
// auth.ErrUserNotProvisionedとして分類されることを確認する
func TestTaskClientV2_List_UserNotProvisioned(t *testing.T) {
	fake := &fakeTaskServiceServer{err: status.Error(codes.PermissionDenied, "user not provisioned")}
	client := newTestTaskClientV2(t, fake)

	_, err := client.List(context.Background(), "token-of-deleted-user", 1, TaskFilter{Limit: 20})
	if !errors.Is(err, auth.ErrUserNotProvisioned) {
		t.Errorf("err = %v, want auth.ErrUserNotProvisioned", err)
	}
}

// PermissionDeniedだが別のメッセージの場合は誤分類しないことを確認する
func TestTaskClientV2_List_PermissionDeniedOtherReason_NotClassifiedAsUserNotProvisioned(t *testing.T) {
	fake := &fakeTaskServiceServer{err: status.Error(codes.PermissionDenied, "some other reason")}
	client := newTestTaskClientV2(t, fake)

	_, err := client.List(context.Background(), "token", 1, TaskFilter{Limit: 20})
	if errors.Is(err, auth.ErrUserNotProvisioned) {
		t.Errorf("err = %v, should not be classified as ErrUserNotProvisioned", err)
	}
}

func TestTaskClientV2_Delete_UnauthenticatedError_ReturnsErrUpstreamUnauthorized(t *testing.T) {
	fake := &fakeTaskServiceServer{err: status.Error(codes.Unauthenticated, "invalid_token")}
	client := newTestTaskClientV2(t, fake)

	err := client.Delete(context.Background(), "bad-token", 1, 1)
	if !errors.Is(err, auth.ErrUpstreamUnauthorized) {
		t.Errorf("err = %v, want auth.ErrUpstreamUnauthorized", err)
	}
}

func TestTaskClientV2_List_Success(t *testing.T) {
	fake := &fakeTaskServiceServer{}
	client := newTestTaskClientV2(t, fake)

	result, err := client.List(context.Background(), "token", 1, TaskFilter{Limit: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != -1 {
		t.Errorf("Total = %d, want -1(v2はtotal不明のため常に-1)", result.Total)
	}
}
