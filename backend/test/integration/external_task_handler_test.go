//go:build integration

// backend/internal/handler/external.TaskHandlerには単体テストが無い(TaskServiceが
// interfaceではなく*repository.Taskを要求する具象型のため、フェイクリポジトリでの純粋な単体テストが組めない)
// CONTRACT.mdのDB設計上この層は実MySQL前提の
// 結合テストで検証するのが既存方針(test/integration配下の他テストと同じ)であるため、
// ここでHTTPハンドラ層(v1/v2のJSON形状・Feature Flagによる振り分けログ)を検証する
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
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/handler/external"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// fakeExternalFlagEvaluator はbackend.external-tasks-pagination-v2・
// backend.external-tasks-orm(CONTRACT.mdセクション24で追加)の評価結果を
// 固定できるフェイク(bff/internal/proxy.fakeFlagEvaluatorと同じ考え方)
type fakeExternalFlagEvaluator struct {
	useV2 bool
	orm   string // "gorm" または "bob"
}

func (f fakeExternalFlagEvaluator) BoolValue(context.Context, string, bool, string) bool {
	return f.useV2
}

func (f fakeExternalFlagEvaluator) StringValue(_ context.Context, _ string, defaultValue string, _ string) string {
	if f.orm == "" {
		return defaultValue
	}
	return f.orm
}

// checkV1Payload/checkV2Payload はoffset(v1)/keyset(v2)、GORM/bobいずれで取得しても
// 同一であるべきレスポンスJSON形状を検証する(CONTRACT.mdセクション24: 「レスポンスのJSON形状は
// 4パターンとも完全に同一」という要件そのものを検証している)
func checkV1Payload(t *testing.T, body []byte) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("レスポンスJSONのパース失敗: %v, body=%s", err, body)
	}
	for _, key := range []string{"tasks", "page", "page_size", "total"} {
		if _, ok := got[key]; !ok {
			t.Errorf("v1のレスポンスに %q が無い。body=%s", key, body)
		}
	}
	if _, ok := got["next_cursor"]; ok {
		t.Errorf("v1のレスポンスにv2専用の next_cursor が含まれている。body=%s", body)
	}
	tasks, _ := got["tasks"].([]any)
	if len(tasks) == 0 {
		t.Errorf("v1のレスポンスにtasksが1件も無い。body=%s", body)
	}
}

func checkV2Payload(t *testing.T, body []byte) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("レスポンスJSONのパース失敗: %v, body=%s", err, body)
	}
	for _, key := range []string{"tasks", "next_cursor", "limit"} {
		if _, ok := got[key]; !ok {
			t.Errorf("v2のレスポンスに %q が無い。body=%s", key, body)
		}
	}
	if _, ok := got["total"]; ok {
		t.Errorf("v2のレスポンスにv1専用の total が含まれている。body=%s", body)
	}
	tasks, _ := got["tasks"].([]any)
	if len(tasks) == 0 {
		t.Errorf("v2のレスポンスにtasksが1件も無い。body=%s", body)
	}
}

// TestExternalTaskHandler_List は、backend.external-tasks-pagination-v2(offset/cursor)×
// backend.external-tasks-orm(gorm/bob)の直交する2軸、4通りの組み合わせが
// それぞれ実際に正しく切り替わり、同じ形状のレスポンスを返すことを確認する
// (CONTRACT.mdセクション24の表そのものをテストケース化している)
func TestExternalTaskHandler_List(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}
	gin.SetMode(gin.TestMode)

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
	taskService := service.NewTaskService(taskRepo)
	ctx := context.Background()

	user, err := userRepo.UpsertByKeycloakSub(ctx, "test-sub-external-handler", "external-handler@example.com", "外部ハンドラ太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}
	// 非冪等バグ(過去にtask_external_pagination_test.go等で発覚済みのパターン)を
	// 避けるため、実行前に既存タスクを削除しておく
	if err := gormDB.Unscoped().Where("user_id = ?", user.ID).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("既存タスクのクリーンアップ失敗: %v", err)
	}
	if _, err := taskService.Create(ctx, user.ID, service.TaskInput{
		Name:       "外部ハンドラテスト用タスク",
		Status:     "waiting",
		FinishedOn: time.Now().AddDate(0, 0, 3),
	}); err != nil {
		t.Fatalf("タスク作成失敗: %v", err)
	}

	tests := []struct {
		name         string
		useV2        bool
		orm          string
		wantImplLog  string
		checkPayload func(t *testing.T, body []byte)
	}{
		{
			name:         "offset×gorm(既定): v1(offsetページング)のJSON形状",
			useV2:        false,
			orm:          "gorm",
			wantImplLog:  "implementation=v1(offsetページング)",
			checkPayload: checkV1Payload,
		},
		{
			name:         "offset×bob: v1(offsetページング)相当をbobで取得してもJSON形状は同一",
			useV2:        false,
			orm:          "bob",
			wantImplLog:  "implementation=v1(offsetページング)",
			checkPayload: checkV1Payload,
		},
		{
			name:         "cursor×gorm: v2(keysetページング)のJSON形状",
			useV2:        true,
			orm:          "gorm",
			wantImplLog:  "implementation=v2(keysetページング)",
			checkPayload: checkV2Payload,
		},
		{
			name:         "cursor×bob: v2(keysetページング)相当をbobで取得してもJSON形状は同一",
			useV2:        true,
			orm:          "bob",
			wantImplLog:  "implementation=v2(keysetページング)",
			checkPayload: checkV2Payload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, nil))
			handler := external.NewTaskHandler(taskService, fakeExternalFlagEvaluator{useV2: tt.useV2, orm: tt.orm}, logger)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/external/v1/tasks?user_id="+strconv.FormatUint(user.ID, 10), nil)
			// RequireExternalClientAuthを経由せず、検証済みclient_idが既にcontextに
			// 入っている状態を模擬する(v1/task_test.goのSetClaimsForTestingと同じ考え方)
			c.Set("external_client_id", "test-external-client")

			handler.List(c)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
			}
			tt.checkPayload(t, w.Body.Bytes())

			log := logBuf.String()
			for _, want := range []string{
				"client_id=test-external-client",
				"use_v2=" + boolString(tt.useV2),
				tt.wantImplLog,
				"orm=" + tt.orm,
			} {
				if !strings.Contains(log, want) {
					t.Errorf("ログに %q が含まれていない。log=%s", want, log)
				}
			}
		})
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
