package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-cmp/cmp"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// fakeUserLookup はuserLookupインターフェースの最小フェイク実装
type fakeUserLookup struct {
	user *model.User
	err  error
}

func (f fakeUserLookup) GetByKeycloakSub(ctx context.Context, keycloakSub string) (*model.User, error) {
	return f.user, f.err
}

func (f fakeUserLookup) Get(ctx context.Context, id uint64) (*model.User, error) {
	return f.user, f.err
}

func TestTaskHandler_resolveUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("claimsがcontextに無ければ401", func(t *testing.T) {
		h := &TaskHandler{users: fakeUserLookup{}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

		_, ok := h.resolveUserID(c)
		if ok {
			t.Fatal("okになるべきではない")
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("userLookupがエラーを返すと403(user_not_provisioned)", func(t *testing.T) {
		h := &TaskHandler{users: fakeUserLookup{err: errors.New("not found")}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"}})

		_, ok := h.resolveUserID(c)
		if ok {
			t.Fatal("okになるべきではない")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})

	t.Run("成功時はuserIDとtrueを返す", func(t *testing.T) {
		h := &TaskHandler{users: fakeUserLookup{user: &model.User{ID: 42}}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "sub-1"}})

		id, ok := h.resolveUserID(c)
		if !ok {
			t.Fatalf("okであるべき, body=%s", w.Body.String())
		}
		if id != 42 {
			t.Errorf("id = %d, want 42", id)
		}
	})

	// CONTRACT.md セクション16.5: ローカル(HMAC/RSA)発行のJWTはKeycloak発行と異なり、
	// subに内部user_idそのものが入っている想定のため、GetByKeycloakSubではなくGetで
	// 解決する分岐が必要(実装時に導入した回帰テスト)
	t.Run("issがローカル発行(HMAC)ならsubを内部user_idとしてGetで解決する", func(t *testing.T) {
		h := &TaskHandler{users: fakeUserLookup{user: &model.User{ID: 7}}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "7", Issuer: authjwt.LocalHMACIssuer},
		})

		id, ok := h.resolveUserID(c)
		if !ok {
			t.Fatalf("okであるべき, body=%s", w.Body.String())
		}
		if id != 7 {
			t.Errorf("id = %d, want 7", id)
		}
	})

	// 【3回目のテスト監査(認可観点)で確認】
	// RequireAuth自体はazp(authorized party) を一切見ないため、外部公開API用に発行されたClient Credentials Grantのトークン
	// (aud=backendが注入されている、CONTRACT.mdセクション8参照)であっても、
	// aud/iss/署名さえ正しければ /internal/v1/tasks のRequireAuthは素通りしてしまう
	// (RequireExternalClientAuthのようなazpチェックはRequireAuthには無い)
	//
	// ただし実際にはこのシナリオでも安全である: Client Credentials Grant の sub は external-api-client のサービスアカウント自身の keycloak_sub であり、
	// これは bff の JITプロビジョニングフロー(セクション10、ユーザーがブラウザでログインした時だけ動く)を一度も通らないため、users テーブルには絶対に対応する行が存在しない
	//
	// そのため resolveUserIDのGetByKeycloakSub が必ず失敗し、403 user_not_provisioned で弾かれる
	//
	// つまり「azpを見ない」という設計の緩さは、「サービスアカウントは決してJIT provisioningされない」という別の不変条件によって実害無く保たれている、
	// という2段構えの安全性であることをテストとして明示的に固定しておく
	// (どちらか一方が将来崩れても、テストが検知できるようにする)
	t.Run("Keycloak発行トークンでも未プロビジョニングのsub(例: 外部クライアントのサービスアカウント)は403", func(t *testing.T) {
		h := &TaskHandler{users: fakeUserLookup{err: errors.New("record not found")}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		// azpがexternal-api-client(Client Credentials Grant由来)のトークンを模す
		// resolveUserID自体はazpを見ないが、それでも403になることが本題
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "service-account-external-api-client", Issuer: "https://keycloak.example/realms/training"},
			Azp:              "external-api-client",
		})

		_, ok := h.resolveUserID(c)
		if ok {
			t.Fatal("okになるべきではない(内部APIへのサービスアカウント由来トークンの転用は防がれるべき)")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})

	t.Run("issがローカル発行だがsubが数値でなければ403", func(t *testing.T) {
		h := &TaskHandler{users: fakeUserLookup{user: &model.User{ID: 7}}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		authjwt.SetClaimsForTesting(c, &authjwt.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "not-a-number", Issuer: authjwt.LocalRSAIssuer},
		})

		_, ok := h.resolveUserID(c)
		if ok {
			t.Fatal("okになるべきではない")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})
}

// taskRequestBody の JSON バインドは、bff 側(taskV1RequestBody)がスネークケースで送ってくることを前提にしている
//
// 統合時に一度 camelCase/snake_case の食い違いで Create/Update が常に400になる不具合が起きたため、この回帰テストで固定する
func TestTaskRequestBody_Binding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "スネークケースの正しいボディはバインドできる",
			body:    `{"name":"買い物","status":"waiting","finished_on":"2030-01-01","label_ids":[1,2]}`,
			wantErr: false,
		},
		{
			name:    "camelCaseのfinishedOnはバインドされずrequired違反で失敗する(回帰テスト)",
			body:    `{"name":"買い物","status":"waiting","finishedOn":"2030-01-01"}`,
			wantErr: true,
		},
		{
			name:    "nameが無いと失敗",
			body:    `{"status":"waiting","finished_on":"2030-01-01"}`,
			wantErr: true,
		},
		{
			name:    "statusが無いと失敗",
			body:    `{"name":"買い物","finished_on":"2030-01-01"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			var body taskRequestBody
			err := c.ShouldBindJSON(&body)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// taskDTOToJSONの出力はbff側(taskV1DTO)がスネークケースで受け取ることを前提にしている
// 中間構造ではなく実際にシリアライズされたJSONのキー名を確認する
// (camelCaseで出していた不具合の回帰テスト)
func TestTaskDTOToJSON(t *testing.T) {
	desc := "詳細"
	dto := service.TaskDTO{
		ID:          1,
		Name:        "買い物",
		Description: &desc,
		Status:      "waiting",
		FinishedOn:  time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Labels:      []service.LabelDTO{{ID: 1, Name: "重要"}},
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	b, err := json.Marshal(taskDTOToJSON(dto))
	if err != nil {
		t.Fatalf("Marshal失敗: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal失敗: %v", err)
	}

	want := map[string]any{
		"id":          float64(1),
		"name":        "買い物",
		"description": "詳細",
		"status":      "waiting",
		"finished_on": "2030-01-01",
		"labels":      []any{map[string]any{"id": float64(1), "name": "重要"}},
		"created_at":  "2026-01-01T00:00:00Z",
		"updated_at":  "2026-01-02T00:00:00Z",
	}
	if diff := cmp.Diff(want, decoded); diff != "" {
		t.Errorf("taskDTOToJSON mismatch (-want +got):\n%s", diff)
	}
}
