package external

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
)

// 【実機検証(外部公開APIへのリクエストログ追加)で発覚したギャップへの回帰テスト】
// external.NewRouter()には今日までルーターレベルのテストが1件も存在しなかった
// (TaskHandler.List自体は個別にテストされうるが、RequireExternalClientAuth・requestLogger・
// clientIDToContextを含めたミドルウェアチェーン全体を通した検証は無かった)
//
// TaskHandlerの内部(*service.TaskService)はconcrete typeで差し替えできないため、
// 認可ミドルウェアで弾かれる経路(ハンドラ本体へ到達しない)だけをここでは検証する
// (ハンドラ本体を通す経路は実DBが要る結合テストの領域であり、このファイルのスコープ外)

type fakeExternalVerifier struct {
	claims *authjwt.Claims
	err    error
}

func (f fakeExternalVerifier) Verify(string) (*authjwt.Claims, error) {
	return f.claims, f.err
}

type fakeFlagEvaluator struct{}

func (fakeFlagEvaluator) BoolValue(context.Context, string, bool, string) bool     { return false }
func (fakeFlagEvaluator) StringValue(context.Context, string, string, string) string { return "gorm" }

func newTestRouterForRejectionPaths(t *testing.T, logBuf *bytes.Buffer, verifier authjwt.TokenVerifier) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(logBuf, nil))
	h := Handlers{Task: NewTaskHandler(nil, fakeFlagEvaluator{}, logger)}
	return NewRouter(h, verifier, "external-api-client", logger)
}

// 【NewRouterのシグネチャ変更(logger引数追加)に対する退行確認】
// Authorizationヘッダ無しなら、requestLoggerやTaskHandlerを一切通らず401になることを確認する
func TestNewRouter_Authorizationヘッダ無しは401(t *testing.T) {
	var logBuf bytes.Buffer
	router := newTestRouterForRejectionPaths(t, &logBuf, fakeExternalVerifier{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/external/v1/tasks?user_id=1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", w.Code, w.Body.String())
	}
}

// azp(発行先クライアント)が期待値と一致しない場合は403になることを確認する
// (RequireExternalClientAuthの既存ロジックだが、logger引数追加後もミドルウェアの並び順・
// 適用順が壊れていないことの確認を兼ねる)
func TestNewRouter_azp不一致は403(t *testing.T) {
	var logBuf bytes.Buffer
	verifier := fakeExternalVerifier{claims: &authjwt.Claims{Azp: "someone-else"}}
	router := newTestRouterForRejectionPaths(t, &logBuf, verifier)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/external/v1/tasks?user_id=1", nil)
	req.Header.Set("Authorization", "Bearer dummy")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
}

// 【本題】requestLoggerが外部公開APIルーターにも今回追加されたことを、実際にログ行が
// 出力されることで確認する(以前は内部REST(v1)にしかこのミドルウェアが無かった)
func TestNewRouter_リクエストログが出力される(t *testing.T) {
	var logBuf bytes.Buffer
	router := newTestRouterForRejectionPaths(t, &logBuf, fakeExternalVerifier{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/external/v1/tasks?user_id=1", nil)
	router.ServeHTTP(w, req)

	line := strings.TrimSpace(logBuf.String())
	if line == "" {
		t.Fatal("requestLoggerによるログ行が1行も出力されていない")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(line, "\n", 2)[0]), &record); err != nil {
		t.Fatalf("ログ行が妥当なJSONではない: %v (line=%q)", err, line)
	}
	if record["method"] != http.MethodGet {
		t.Errorf(`record["method"] = %v, want GET`, record["method"])
	}
	if record["path"] != "/external/v1/tasks" {
		t.Errorf(`record["path"] = %v, want /external/v1/tasks`, record["path"])
	}
	if record["status"] != float64(http.StatusUnauthorized) {
		t.Errorf(`record["status"] = %v, want 401`, record["status"])
	}
}
