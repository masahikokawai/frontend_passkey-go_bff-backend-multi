//go:build integration

// gRPC v2(TaskServer)がJWT検証interceptor込みで実際に正しく応答することを、
// bufconn(インメモリのgRPCリスナー)を使って確認する結合テスト
// 実行前提: 環境変数 TEST_DB_DSN(README参照)
// JWKSは実Keycloakを使わず、このテスト内でRSA鍵ペア+フェイクのJWKSエンドポイントを用意する
// (authjwtパッケージの単体テストと同じ考え方)
package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	taskv1 "github.com/masahikokawai/training-go/bff-gin/backend/gen/task/v1"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/grpcserver"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

func TestGRPCTaskServer_AuthAndCRUD(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	// --- フェイクJWKS + RSA鍵ペア ---
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("RSA鍵生成に失敗: %v", err)
	}
	const kid = "grpc-test-kid"
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.PublicKey.E)).Bytes())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{"kty": "RSA", "kid": kid, "use": "sig", "n": n, "e": e}},
		})
	}))
	defer jwksServer.Close()

	const issuer = "http://keycloak.test/realms/training"
	const audience = "backend"
	keycloakVerifier := authjwt.NewVerifier(jwksServer.URL, issuer, audience)

	// ローカル(HMAC)発行トークンのresolveUserID分岐(CONTRACT.mdセクション16.5)も
	// このgRPC結合テストで検証するため、本番と同じくDispatcherで両方式を束ねる
	const localHMACSecret = "grpc-test-local-hmac-secret"
	dispatcher := authjwt.NewDispatcher().
		Register(issuer, keycloakVerifier).
		Register(authjwt.LocalHMACIssuer, authjwt.NewHMACVerifier(localHMACSecret, authjwt.LocalHMACIssuer, audience))

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

	const keycloakSub = "grpc-test-sub"
	_, err = userRepo.UpsertByKeycloakSub(context.Background(), keycloakSub, "grpc-test@example.com", "gRPCテスト太郎", model.RoleGeneral)
	if err != nil {
		t.Fatalf("テストユーザー作成失敗: %v", err)
	}

	// ローカル(HMAC)認証ユーザー相当の行
	// sub=内部 user_id そのものなので keycloak_sub は持たない(user_keycloaksテーブルに行を作らない)
	// 固定emailで再実行してもMySQL error 1062にならないよう前回分を先に消しておく
	// (task_external_pagination_test.goと同じ「何度実行しても同じ結果になる」ための後始末)
	const localUserEmail = "grpc-local-test@example.com"
	if err := gormDB.Unscoped().Where("user_id IN (?)",
		gormDB.Model(&model.User{}).Select("id").Where("email = ?", localUserEmail)).
		Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("前回テストデータの後始末(tasks)に失敗: %v", err)
	}
	if err := gormDB.Unscoped().Where("email = ?", localUserEmail).Delete(&model.User{}).Error; err != nil {
		t.Fatalf("前回テストデータの後始末(users)に失敗: %v", err)
	}
	localUser := model.User{Email: localUserEmail, Name: "gRPCローカルテスト", Role: model.RoleGeneral}
	if err := gormDB.Create(&localUser).Error; err != nil {
		t.Fatalf("ローカルテストユーザー作成失敗: %v", err)
	}

	// --- bufconnでgRPCサーバーを起動 ---
	lis := bufconn.Listen(1024 * 1024)
	taskServer := grpcserver.NewTaskServer(taskService, userRepo)
	srv := grpcserver.New(dispatcher, taskServer, slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("gRPC接続失敗: %v", err)
	}
	defer conn.Close()
	client := taskv1.NewTaskServiceClient(conn)

	mintToken := func(sub string) string {
		now := time.Now()
		claims := jwt.MapClaims{
			"iss": issuer, "aud": audience, "sub": sub,
			"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = kid
		signed, err := token.SignedString(priv)
		if err != nil {
			t.Fatalf("トークン署名失敗: %v", err)
		}
		return signed
	}
	mintLocalHMACToken := func(sub string) string {
		now := time.Now()
		claims := jwt.MapClaims{
			"iss": authjwt.LocalHMACIssuer, "aud": audience, "sub": sub,
			"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte(localHMACSecret))
		if err != nil {
			t.Fatalf("ローカルHMACトークン署名失敗: %v", err)
		}
		return signed
	}
	authedCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+mintToken(keycloakSub))

	t.Run("ローカル(HMAC)発行トークンはsubを内部user_idとして解決する", func(t *testing.T) {
		ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization",
			"Bearer "+mintLocalHMACToken(strconv.FormatUint(localUser.ID, 10)))
		finishedOn := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
		created, err := client.CreateTask(ctx, &taskv1.CreateTaskRequest{
			Name: "ローカルHMAC経由のタスク", Status: "waiting", FinishedOn: finishedOn,
		})
		if err != nil {
			t.Fatalf("CreateTask失敗: %v", err)
		}

		listResp, err := client.ListTasks(ctx, &taskv1.ListTasksRequest{Limit: 50})
		if err != nil {
			t.Fatalf("ListTasks失敗: %v", err)
		}
		found := false
		for _, task := range listResp.GetTasks() {
			if task.GetId() == created.GetId() {
				found = true
			}
		}
		if !found {
			t.Errorf("ローカルHMACユーザーで作成したタスク(id=%d)が同ユーザーの一覧に含まれていない", created.GetId())
		}

		// subが数値でない(=内部user_idとして解釈できない)場合は403相当(PermissionDenied)
		ctxBadSub := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+mintLocalHMACToken("not-a-number"))
		if _, err := client.ListTasks(ctxBadSub, &taskv1.ListTasksRequest{Limit: 10}); err == nil {
			t.Error("subが数値でないローカルトークンはエラーになるべきだが成功した")
		}
	})

	t.Run("認証無しはUnauthenticated", func(t *testing.T) {
		_, err := client.ListTasks(context.Background(), &taskv1.ListTasksRequest{Limit: 10})
		if err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("未知のユーザーのトークンはPermissionDenied", func(t *testing.T) {
		ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+mintToken("not-provisioned-sub"))
		_, err := client.ListTasks(ctx, &taskv1.ListTasksRequest{Limit: 10})
		if err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("CreateTask→ListTasksで作成したタスクが取得できる", func(t *testing.T) {
		finishedOn := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
		created, err := client.CreateTask(authedCtx, &taskv1.CreateTaskRequest{
			Name:       "gRPC経由のタスク",
			Status:     "waiting",
			FinishedOn: finishedOn,
		})
		if err != nil {
			t.Fatalf("CreateTask失敗: %v", err)
		}
		if created.GetName() != "gRPC経由のタスク" {
			t.Errorf("Name = %q, want gRPC経由のタスク", created.GetName())
		}

		listResp, err := client.ListTasks(authedCtx, &taskv1.ListTasksRequest{Limit: 50})
		if err != nil {
			t.Fatalf("ListTasks失敗: %v", err)
		}
		found := false
		for _, task := range listResp.GetTasks() {
			if task.GetId() == created.GetId() {
				found = true
			}
		}
		if !found {
			t.Errorf("作成したタスク(id=%d)が一覧に含まれていない", created.GetId())
		}

		t.Run("UpdateTask→GetTaskで更新後の値が取得できる", func(t *testing.T) {
			updated, err := client.UpdateTask(authedCtx, &taskv1.UpdateTaskRequest{
				Id:         created.GetId(),
				Name:       "gRPC経由のタスク(更新後)",
				Status:     "completed",
				FinishedOn: finishedOn,
			})
			if err != nil {
				t.Fatalf("UpdateTask失敗: %v", err)
			}
			if updated.GetStatus() != "completed" {
				t.Errorf("Status = %q, want completed", updated.GetStatus())
			}

			got, err := client.GetTask(authedCtx, &taskv1.GetTaskRequest{Id: created.GetId()})
			if err != nil {
				t.Fatalf("GetTask失敗: %v", err)
			}
			if got.GetName() != "gRPC経由のタスク(更新後)" {
				t.Errorf("Name = %q, want 更新後の名前", got.GetName())
			}
		})

		t.Run("DeleteTask後はGetTaskがNotFoundになる", func(t *testing.T) {
			if _, err := client.DeleteTask(authedCtx, &taskv1.DeleteTaskRequest{Id: created.GetId()}); err != nil {
				t.Fatalf("DeleteTask失敗: %v", err)
			}
			if _, err := client.GetTask(authedCtx, &taskv1.GetTaskRequest{Id: created.GetId()}); err == nil {
				t.Fatal("削除後のGetTaskはエラーになるべきだが成功した")
			}
		})
	})
}
