//go:build integration

// REST v1(/internal/v1/tasks)を、実際のHTTPサーバー・実DB・本番と同じ
// authjwt.Dispatcher(iss振り分け)を通して検証する
//
// これまでローカル(HMAC/RSA)認証のissuer分岐(resolveUserID、CONTRACT.mdセクション16.5)は
// - handler/v1/task_test.go でフェイクclaimsを注入するユニットテスト
// - test/integration/grpc_taskserver_test.go で gRPC v2 + 実Dispatcher + 実DB
// の2種類でしか検証されておらず、「REST v1をHTTP経由で実際に叩いたときに
// 本物のDispatcherが正しくローカルHMACトークンを検証し、内部user_idを解決する」
// という一連の流れそのものは未検証だった(コーディネーターの手動curl確認でしか通っていなかった)
// このファイルはその欠落を埋める
//
// 実行前提: 環境変数 TEST_DB_DSN(README参照)
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	v1 "github.com/masahikokawai/training-go/bff-gin/backend/internal/handler/v1"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestTaskV1_LocalHMACAuth_EndToEnd(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	const hmacSecret = "test-e2e-hmac-secret"
	const audience = "backend"

	gormDB, err := db.New(dsn, slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}
	// 【テスト監査で発見・修正】このDB接続をCloseせずにテストが終わっていたため、
	// 実行のたびに接続がリークしていた(internal/db/db.goのコメントで呼び出し側の責務と明記)
	if sqlDB, sqlErr := gormDB.DB(); sqlErr == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	userRepo := repository.NewUser(gormDB)
	taskRepo := repository.NewTask(gormDB)
	labelRepo := repository.NewLabel(gormDB)
	featureFlagRepo := repository.NewFeatureFlag(gormDB)

	// 固定emailで、実行の都度前回分を消してから作り直す(既存の統合テストの
	// 冪等化パターンに合わせる)
	const testEmail = "task-v1-local-hmac-e2e@example.com"
	if existingUser, _, err := userRepo.GetByEmail(context.Background(), testEmail); err == nil && existingUser != nil {
		gormDB.Unscoped().Where("user_id = ?", existingUser.ID).Delete(&model.Task{})
		gormDB.Unscoped().Delete(&model.User{}, existingUser.ID)
	}
	newUser := model.User{Email: testEmail, Name: "E2Eテスト太郎", Role: model.RoleGeneral}
	if err := gormDB.Create(&newUser).Error; err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	t.Cleanup(func() {
		gormDB.Unscoped().Where("user_id = ?", newUser.ID).Delete(&model.Task{})
		gormDB.Unscoped().Delete(&model.User{}, newUser.ID)
	})

	dispatcher := authjwt.NewDispatcher().
		Register(authjwt.LocalHMACIssuer, authjwt.NewHMACVerifier(hmacSecret, authjwt.LocalHMACIssuer, audience))

	handlers := v1.Handlers{
		Task:        v1.NewTaskHandler(service.NewTaskService(taskRepo), userRepo),
		Label:       v1.NewLabelHandler(service.NewLabelService(labelRepo)),
		User:        v1.NewUserHandler(service.NewUserService(userRepo)),
		FeatureFlag: v1.NewFeatureFlagExportHandler(featureFlagRepo),
		LocalAuth:   v1.NewLocalAuthHandler(service.NewLocalAuthService(userRepo)),
	}
	router := v1.NewRouter(handlers, dispatcher, slog.New(slog.NewJSONHandler(os.Stdout, nil)), "unused-poll-token", "unused-internal-token", "unused-admin-token", "unused-webauthn-token")
	server := httptest.NewServer(router)
	defer server.Close()

	// bffが発行するローカルHMACトークンと同じ形(iss/aud/sub=内部user_id/exp)
	mint := func(sub string, exp time.Time) string {
		claims := jwt.MapClaims{
			"iss": authjwt.LocalHMACIssuer,
			"aud": audience,
			"sub": sub,
			"exp": exp.Unix(),
			"iat": time.Now().Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte(hmacSecret))
		if err != nil {
			t.Fatalf("トークン署名失敗: %v", err)
		}
		return signed
	}

	doReq := func(method, path, token string, body []byte) *http.Response {
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("リクエスト作成失敗: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("リクエスト送信失敗: %v", err)
		}
		return resp
	}

	sub := strconv.FormatUint(newUser.ID, 10)
	validToken := mint(sub, time.Now().Add(time.Hour))

	t.Run("ローカルHMACトークンでタスク作成→一覧取得まで通しで動く(issuer判定→内部user_id解決)", func(t *testing.T) {
		createBody, _ := json.Marshal(map[string]any{
			"name":        "E2E経由で作成したタスク",
			"status":      "waiting",
			"finished_on": "2030-01-01",
			"label_ids":   []uint64{},
		})
		createResp := doReq(http.MethodPost, "/internal/v1/tasks", validToken, createBody)
		defer createResp.Body.Close()
		if createResp.StatusCode != http.StatusCreated {
			t.Fatalf("Create status = %d, want 201", createResp.StatusCode)
		}

		listResp := doReq(http.MethodGet, "/internal/v1/tasks", validToken, nil)
		defer listResp.Body.Close()
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("List status = %d, want 200", listResp.StatusCode)
		}
		var out struct {
			Tasks []struct {
				Name string `json:"name"`
			} `json:"tasks"`
		}
		if err := json.NewDecoder(listResp.Body).Decode(&out); err != nil {
			t.Fatalf("レスポンスのデコード失敗: %v", err)
		}
		if len(out.Tasks) != 1 || out.Tasks[0].Name != "E2E経由で作成したタスク" {
			t.Errorf("List結果 = %+v, want 作成した1件のみ(他ユーザーのタスクが混入していないこと)", out.Tasks)
		}
	})

	t.Run("subが数値でない(内部user_idとして解釈できない)ローカルトークンは403", func(t *testing.T) {
		// 署名検証自体は通っている(HMAC secret一致・iss/aud/exp正常)ため、authjwt 層は401にしない
		// resolveUserID側で「有効なトークンだが紐づくユーザーが存在しない」と判断して Keycloak 経路の未知 keycloak_sub と同じ
		// user_not_provisioned(403)を返す(backend/internal/handler/v1/task.go参照)
		badToken := mint("not-a-number", time.Now().Add(time.Hour))
		resp := doReq(http.MethodGet, "/internal/v1/tasks", badToken, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("有効期限切れのローカルHMACトークンは401", func(t *testing.T) {
		expiredToken := mint(sub, time.Now().Add(-time.Hour))
		resp := doReq(http.MethodGet, "/internal/v1/tasks", expiredToken, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("Authorizationヘッダ無しは401", func(t *testing.T) {
		resp := doReq(http.MethodGet, "/internal/v1/tasks", "", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})
}
