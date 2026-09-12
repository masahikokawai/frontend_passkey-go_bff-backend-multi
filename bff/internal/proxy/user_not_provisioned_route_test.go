package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
)

// 【実機検証で発覚した不具合の回帰テスト】
// admin画面でログイン中のユーザーを削除すると、
// backendはauth.ErrUserNotProvisioned を返す
// (classifyStatus_test.go・classifyGRPCErr_test.go・Refresher.Doのテストではこの分類・セッション破棄までは検証済みだが、
// 実際にHTTPハンドラ(TaskRoutes.do / LabelRoutes.do)がこれを401 JSONへ変換するところまでは未検証だった)
// gin.Engine + 実際のCookie付きHTTPリクエストで、この一連の流れ全体を検証する

// noopTokenRefresher は ErrUserNotProvisioned の経路では Refresh が呼ばれないはずなので、
// 呼ばれたら即失敗させることでその前提を保証するstub
type noopTokenRefresher struct{ t *testing.T }

func (n noopTokenRefresher) Refresh(context.Context, string) (*auth.TokenSet, error) {
	n.t.Fatal("Refresh()が呼ばれた: ErrUserNotProvisionedの場合はリフレッシュを試みないはず")
	return nil, nil
}

func newTestStoreForProxy(t *testing.T) *auth.Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return auth.NewStore(client)
}

const testSessionCookieName = "session_id"

func createTestSession(t *testing.T, store *auth.Store, authMode string) (sessionID string) {
	t.Helper()
	sessionID, err := store.Create(context.Background(), auth.Session{
		UserID:          1,
		AuthMode:        authMode,
		AccessToken:     "access-token-of-deleted-user",
		RefreshToken:    "refresh-token",
		RefreshTokenExp: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}
	return sessionID
}

// fakeUserGoneTaskBackend はListが常にauth.ErrUserNotProvisionedを返すstub
// (admin画面で削除されたユーザーのセッションでbackendを呼んだ場合を模す)
type fakeUserGoneTaskBackend struct{}

func (fakeUserGoneTaskBackend) List(context.Context, string, uint64, TaskFilter) (TaskListResult, error) {
	return TaskListResult{}, auth.ErrUserNotProvisioned
}
func (fakeUserGoneTaskBackend) Create(context.Context, string, uint64, TaskInput) (Task, error) {
	return Task{}, auth.ErrUserNotProvisioned
}
func (fakeUserGoneTaskBackend) Get(context.Context, string, uint64, uint64) (Task, error) {
	return Task{}, auth.ErrUserNotProvisioned
}
func (fakeUserGoneTaskBackend) Update(context.Context, string, uint64, uint64, TaskInput) (Task, error) {
	return Task{}, auth.ErrUserNotProvisioned
}
func (fakeUserGoneTaskBackend) Delete(context.Context, string, uint64, uint64) error {
	return auth.ErrUserNotProvisioned
}

func TestTaskRoutes_List_UserNotProvisioned_Returns401AndDeletesSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTestStoreForProxy(t)
	sessionID := createTestSession(t, store, auth.AuthModeLocalHMAC)

	routes := &TaskRoutes{
		Clients: map[string]TaskBackendClient{
			"go:rest": fakeUserGoneTaskBackend{},
			"go:grpc": fakeUserGoneTaskBackend{},
		},
		Flags:     &fakeFlagEvaluator{},
		Refresher: auth.NewRefresher(store, noopTokenRefresher{t: t}),
	}

	router := gin.New()
	cookieCfg := auth.CookieConfig{SessionCookieName: testSessionCookieName, CSRFCookieName: "csrf_token"}
	router.GET("/api/tasks", auth.RequireSession(store, cookieCfg), routes.List)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスボディのJSONパースに失敗: %v", err)
	}
	if body["error"] != "unauthenticated" {
		t.Errorf(`body["error"] = %q, want "unauthenticated"`, body["error"])
	}

	// Refresher.Doがセッションを破棄しているはず(次にこのCookieで来ても未ログイン扱いになる)
	if _, err := store.Get(context.Background(), sessionID); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("session should be deleted, Get() error = %v", err)
	}
}

// fakeUserGoneLabelBackend はListが常にauth.ErrUserNotProvisionedを返すstub
type fakeUserGoneLabelBackend struct{}

func (fakeUserGoneLabelBackend) List(context.Context, string) ([]Label, error) {
	return nil, auth.ErrUserNotProvisioned
}

func TestLabelRoutes_List_UserNotProvisioned_Returns401AndDeletesSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTestStoreForProxy(t)
	sessionID := createTestSession(t, store, auth.AuthModeKeycloak)

	// LabelRoutes.Clientは具体型(*LabelClientV1)を要求するため、resty経由でbackend自体を
	// 偽装する(fakeバックエンドサーバーが常に403 user_not_provisionedを返す)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"user_not_provisioned"}`))
	}))
	t.Cleanup(backend.Close)

	routes := &LabelRoutes{
		Client:    NewLabelClientV1(backend.URL),
		Refresher: auth.NewRefresher(store, noopTokenRefresher{t: t}),
	}

	router := gin.New()
	cookieCfg := auth.CookieConfig{SessionCookieName: testSessionCookieName, CSRFCookieName: "csrf_token"}
	router.GET("/api/labels", auth.RequireSession(store, cookieCfg), routes.List)

	req := httptest.NewRequest(http.MethodGet, "/api/labels", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("レスポンスボディのJSONパースに失敗: %v", err)
	}
	if body["error"] != "unauthenticated" {
		t.Errorf(`body["error"] = %q, want "unauthenticated"`, body["error"])
	}

	if _, err := store.Get(context.Background(), sessionID); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("session should be deleted, Get() error = %v", err)
	}
}
