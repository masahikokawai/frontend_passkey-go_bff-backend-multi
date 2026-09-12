package grpcserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	taskv1 "github.com/masahikokawai/training-go/bff-gin/backend/gen/task/v1"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
)

// fakeVerifier はauthjwt.TokenVerifierの最小フェイク実装
// (常に"invalid_token"エラーを返す=どんなAuthorizationヘッダを付けても認証は必ず失敗する、
// このファイルのテストでは「認可interceptorが今も効いているか」だけを確認したいため、
// 実際にトークンを検証成功させる必要はない)
type fakeVerifier struct{}

func (fakeVerifier) Verify(string) (*authjwt.Claims, error) {
	return nil, errors.New("invalid_token")
}

// newBufconnClient はNew()が組み立てたgrpc.Serverをbufconn(インメモリリスナー)で起動し、
// そこへ繋いだクライアントを返す。t.Cleanupでサーバー・接続とも確実に後始末する
func newBufconnClient(t *testing.T, srv *grpc.Server) taskv1.TaskServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return taskv1.NewTaskServiceClient(conn)
}

// 【実機検証(リクエストログ追加)で発覚したリスクへの回帰テスト】
// grpc.ChainUnaryInterceptor(loggingUnaryInterceptor, authjwt.UnaryServerInterceptor)への
// 変更前は grpc.UnaryInterceptor(authjwt.UnaryServerInterceptor) 単体だった
// ログ用interceptorをチェーンの先頭に追加したことで、認可interceptor自体が
// 呼ばれなくなる・スキップされる、という退行が無いことを確認する
// (TaskServerはzero-value{}のままでよい: 未認証で拒否される経路はTaskServer自体を
// 一切呼ばないため、実DB接続が無くても安全に検証できる)
func TestNew_ロギングinterceptor追加後も認可interceptorは引き続き機能する(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	srv := New(fakeVerifier{}, &TaskServer{}, logger)
	client := newBufconnClient(t, srv)

	_, err := client.ListTasks(context.Background(), &taskv1.ListTasksRequest{Limit: 10})

	if err == nil {
		t.Fatal("認可ヘッダ無しのリクエストが成功してしまった(authjwt.UnaryServerInterceptorが機能していない)")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("status.Code(err) = %v, want Unauthenticated", status.Code(err))
	}
}

// 【実機検証で発覚したリスクへの回帰テスト、続き】
// ログ行に記録される status が、認可interceptorが実際に返したエラーコードと一致することを確認する
// (loggingUnaryInterceptorがチェーンの先頭にあるため、内側のauthInterceptorが返したerrを
// そのまま受け取ってログに反映できているか=チェーンの順序・配線が壊れていないかの検証)
func TestNew_認可エラーのステータスコードがログに正しく記録される(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	srv := New(fakeVerifier{}, &TaskServer{}, logger)
	client := newBufconnClient(t, srv)

	_, _ = client.ListTasks(context.Background(), &taskv1.ListTasksRequest{Limit: 10})

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "Unauthenticated") {
		t.Fatalf("ログに実際のgRPCステータス(Unauthenticated)が記録されていない: %s", logOutput)
	}

	var record map[string]any
	firstLine := strings.SplitN(logOutput, "\n", 2)[0]
	if err := json.Unmarshal([]byte(firstLine), &record); err != nil {
		t.Fatalf("ログ行が単一行の妥当なJSONではない: %v (line=%q)", err, firstLine)
	}
	if record["status"] != "Unauthenticated" {
		t.Errorf(`record["status"] = %v, want "Unauthenticated"`, record["status"])
	}
	if record["method"] != "/task.v1.TaskService/ListTasks" {
		t.Errorf(`record["method"] = %v, want "/task.v1.TaskService/ListTasks"`, record["method"])
	}
}

// 【ログインジェクション対策の回帰テスト】
// gRPCのmethod名はクライアントが自由に選べる値ではなく.protoで固定されたRPC名であり、
// このファイルのrequestLoggerが記録する値の中で唯一「外部から渡ってくる文字列」に近いのは
// 無い(REST側のpathとは違いgRPCにはURLパスに相当するクライアント可変フィールドが
// method名自体には存在しない)。念のため、method名にたまたま制御文字(改行等)が
// 含まれていたとしても、slog.NewJSONHandlerが正しくJSONエスケープし、
// ログ行が複数行に分裂しない(=偽のログ行を注入できない)ことを直接確認する
func TestLoggingUnaryInterceptor_method名に改行が含まれてもログは1行のJSONのまま(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	interceptor := loggingUnaryInterceptor(logger)

	maliciousMethod := "/task.v1.TaskService/ListTasks\"}\n{\"level\":\"FORGED\",\"msg\":\"injected"
	handler := func(ctx context.Context, req any) (any, error) { return nil, nil }
	info := &grpc.UnaryServerInfo{FullMethod: maliciousMethod}

	if _, err := interceptor(context.Background(), nil, info, handler); err != nil {
		t.Fatalf("interceptor() error = %v", err)
	}

	lines := strings.Split(strings.TrimRight(logBuf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("ログ出力が複数行に分裂した(ログインジェクションの疑い): %d行, 内容=%q", len(lines), logBuf.String())
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("ログ行が妥当なJSONではない(エスケープ漏れの疑い): %v", err)
	}
	if record["method"] != maliciousMethod {
		t.Errorf("method フィールドが改行込みでそのまま(エスケープされた形で)記録されていない: %v", record["method"])
	}
}

// 【今回の変更の主目的そのものへの回帰テスト】
// 正常応答時はstatus=OKとしてログされることを確認する(エラー時のUnauthenticatedとの対比)
func TestLoggingUnaryInterceptor_正常応答はstatusOKでログする(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	interceptor := loggingUnaryInterceptor(logger)

	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/task.v1.TaskService/ListTasks"}

	if _, err := interceptor(context.Background(), nil, info, handler); err != nil {
		t.Fatalf("interceptor() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(logBuf.Bytes(), &record); err != nil {
		t.Fatalf("ログ行のJSONパースに失敗: %v", err)
	}
	if record["status"] != "OK" {
		t.Errorf(`record["status"] = %v, want "OK"`, record["status"])
	}
}
