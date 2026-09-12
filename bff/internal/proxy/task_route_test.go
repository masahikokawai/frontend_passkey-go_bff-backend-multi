package proxy

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/go-cmp/cmp"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
)

// fakeTaskBackend はTaskBackendClientの手書きstub
// どの実装が呼ばれたかを記録する
type fakeTaskBackend struct {
	name       string
	listResult TaskListResult
	listCalled bool
}

func (f *fakeTaskBackend) List(_ context.Context, _ string, _ uint64, filter TaskFilter) (TaskListResult, error) {
	f.listCalled = true
	return f.listResult, nil
}
func (f *fakeTaskBackend) Create(context.Context, string, uint64, TaskInput) (Task, error) {
	return Task{}, nil
}
func (f *fakeTaskBackend) Get(context.Context, string, uint64, uint64) (Task, error) {
	return Task{}, nil
}
func (f *fakeTaskBackend) Update(context.Context, string, uint64, uint64, TaskInput) (Task, error) {
	return Task{}, nil
}
func (f *fakeTaskBackend) Delete(context.Context, string, uint64, uint64) error { return nil }

// fakeFlagEvaluator は backend.task-language / backend.task-protocol に対して
// 固定値を返すstub。空文字の場合はdefaultValueをそのまま返す(pickClient側の既定値を尊重する)
type fakeFlagEvaluator struct {
	language string
	protocol string
}

func (f *fakeFlagEvaluator) StringValue(_ context.Context, flagKey string, defaultValue string, _ string) string {
	switch flagKey {
	case "backend.task-language":
		if f.language == "" {
			return defaultValue
		}
		return f.language
	case "backend.task-protocol":
		if f.protocol == "" {
			return defaultValue
		}
		return f.protocol
	default:
		return defaultValue
	}
}

func TestTaskRoutes_pickClient(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		wantV2   bool
	}{
		{name: "protocol=restならgo:rest(v1)が選ばれる", protocol: "rest", wantV2: false},
		{name: "protocol=grpcならgo:grpc(v2)が選ばれる", protocol: "grpc", wantV2: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1 := &fakeTaskBackend{name: "v1", listResult: TaskListResult{Total: 1}}
			v2 := &fakeTaskBackend{name: "v2", listResult: TaskListResult{Total: 2}}
			routes := &TaskRoutes{
				Clients: map[string]TaskBackendClient{
					"go:rest": v1,
					"go:grpc": v2,
				},
				Flags: &fakeFlagEvaluator{language: "go", protocol: tt.protocol},
			}

			client := routes.pickClient(context.Background(), 1)
			got, err := client.List(context.Background(), "token", 1, TaskFilter{Limit: 20})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			wantClient := v1
			if tt.wantV2 {
				wantClient = v2
			}
			if diff := cmp.Diff(wantClient.listResult, got); diff != "" {
				t.Errorf("pickClient()が期待した実装を選ばなかった (-want +got):\n%s", diff)
			}
			if tt.wantV2 && !v2.listCalled {
				t.Error("go:grpc(v2)が呼ばれることを期待したが呼ばれなかった")
			}
			if !tt.wantV2 && !v1.listCalled {
				t.Error("go:rest(v1)が呼ばれることを期待したが呼ばれなかった")
			}
		})
	}
}

// CONTRACT.mdセクション20.9: 各言語実装は段階的に追加していくため、backend.task-languageが
// まだ実装されていない言語(rust等)を指している間は、pickClientがgoへフォールバックすることを確認する
func TestTaskRoutes_pickClient_未実装言語はgoにフォールバックする(t *testing.T) {
	v1 := &fakeTaskBackend{name: "v1", listResult: TaskListResult{Total: 1}}
	v2 := &fakeTaskBackend{name: "v2", listResult: TaskListResult{Total: 2}}
	var buf bytes.Buffer
	routes := &TaskRoutes{
		Clients: map[string]TaskBackendClient{
			"go:rest": v1,
			"go:grpc": v2,
		},
		Flags:  &fakeFlagEvaluator{language: "rust", protocol: "grpc"},
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
	}

	client := routes.pickClient(context.Background(), 1)
	got, err := client.List(context.Background(), "token", 1, TaskFilter{Limit: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if diff := cmp.Diff(v2.listResult, got); diff != "" {
		t.Errorf("フォールバック先がgo:grpcになっていない (-want +got):\n%s", diff)
	}

	log := buf.String()
	for _, want := range []string{"requested_key=rust:grpc", "fallback_key=go:grpc"} {
		if !strings.Contains(log, want) {
			t.Errorf("フォールバックのログに %q が含まれていない。log=%s", want, log)
		}
	}
}

// admin/go・admin/railsでbackend.task-language/backend.task-protocolをDB上で切り替えたときに、
// 実際にどの実装が使われたかログから追えることを確認する(切り替えの可視化)
func TestTaskRoutes_pickClient_ログに振り分け結果を残す(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		wantImpl string
	}{
		{name: "protocol=rest", protocol: "rest", wantImpl: "go:rest"},
		{name: "protocol=grpc", protocol: "grpc", wantImpl: "go:grpc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			routes := &TaskRoutes{
				Clients: map[string]TaskBackendClient{
					"go:rest": &fakeTaskBackend{},
					"go:grpc": &fakeTaskBackend{},
				},
				Flags:  &fakeFlagEvaluator{language: "go", protocol: tt.protocol},
				Logger: slog.New(slog.NewTextHandler(&buf, nil)),
			}

			routes.pickClient(context.Background(), 42)

			log := buf.String()
			for _, want := range []string{
				"user_id=42",
				"language=go",
				"protocol=" + tt.protocol,
				"implementation=" + tt.wantImpl,
			} {
				if !strings.Contains(log, want) {
					t.Errorf("ログに %q が含まれていない。log=%s", want, log)
				}
			}
		})
	}
}

// LoggerがnilでもpickClientがpanicしないことを確認する回帰テスト
// (main.go以外の既存呼び出し・既存テストがLoggerを設定せずにTaskRoutes{}を組み立てる
// ケースへの後方互換)
func TestTaskRoutes_pickClient_Loggerがnilでもpanicしない(t *testing.T) {
	routes := &TaskRoutes{
		Clients: map[string]TaskBackendClient{
			"go:rest": &fakeTaskBackend{},
			"go:grpc": &fakeTaskBackend{},
		},
		Flags: &fakeFlagEvaluator{language: "go", protocol: "grpc"},
	}
	_ = routes.pickClient(context.Background(), 1)
}

// newContextWithSession はauth.RequireSessionミドルウェア(Redis参照)を経由せず、
// gin.Contextへ直接セッションを積んでハンドラを単体テストするためのヘルパー
//
// 【テスト監査で追記】Create/Updateがボディのバインドで早期リターンする経路
// (ShouldBindJSONの失敗)は、Refresher/Clients/Flagsのいずれにも到達しないため、
// それらは組み立てずに済む(セッションの存在チェックだけ通過させればよい)
func newContextWithSession(w *httptest.ResponseRecorder, req *http.Request) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	// auth.RequireSessionがgin.Contextへ積むキー名(内部定数だが文字列キーのため
	// パッケージ外からも同じ文字列で直接Setできる)と一致させる
	c.Set("auth.session", &auth.Session{UserID: 1})
	c.Set("auth.session_id", "test-session-id")
	return c
}

// 【テスト監査で発見】bff全体でJSONボディのバインド失敗(不正なJSON・型の不一致)を
// 実際にリクエストとして送り、raw 500やpanicにならず一貫して400になることを確認する
// テストが1件も無かった(コード上はShouldBindJSONの結果を全箇所400にマッピングしている
// ように見えるが、実際にリクエストを通した回帰テストが無いと将来の変更で崩れても気づけない)
func TestTaskRoutes_Create_不正なJSONボディは400になりpanicしない(t *testing.T) {
	routes := &TaskRoutes{}
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{not valid json truncated`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c := newContextWithSession(w, req)

	routes.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestTaskRoutes_Create_フィールドの型が不一致でも400になりpanicしない(t *testing.T) {
	routes := &TaskRoutes{}
	// name が文字列であるべきところに数値を送る(JSONとしては妥当だが型が違う)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"name":12345,"status":"waiting"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c := newContextWithSession(w, req)

	routes.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestTaskRoutes_Create_空ボディは400になりpanicしない(t *testing.T) {
	routes := &TaskRoutes{}
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(``))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c := newContextWithSession(w, req)

	routes.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// Content-Typeを送らない/違う値を送っても(Ginのjsonバインディングはヘッダーに関わらず
// 常にJSONとして解釈しようとする)、少なくとも400のままでありpanicしないことを確認する
func TestTaskRoutes_Create_ContentTypeが無くても400になりpanicしない(t *testing.T) {
	routes := &TaskRoutes{}
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{not valid json`))
	// Content-Typeを意図的に設定しない
	w := httptest.NewRecorder()
	c := newContextWithSession(w, req)

	routes.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}
