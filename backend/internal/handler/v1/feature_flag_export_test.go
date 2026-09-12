package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// RequireFeatureFlagPollTokenは、
// bff の GO Feature Flag HTTP retriever からの定期ポーリングを、通常の JWT(RequireAuth)とは別の共有シークレットで認可する
// (CONTRACT.mdセクション13)
//
// ここではミドルウェア単体の401/通過の分岐だけを検証する
// (Exportハンドラ自体はGORM接続が必要なため、実DB結合テストの対象とする)
func TestRequireFeatureFlagPollToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "ヘッダ無し", header: "", wantStatus: http.StatusUnauthorized},
		{name: "値が不一致", header: "wrong-token", wantStatus: http.StatusUnauthorized},
		{name: "値が一致", header: "expected-token", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/export", RequireFeatureFlagPollToken("expected-token"), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/export", nil)
			if tt.header != "" {
				req.Header.Set("X-Feature-Flag-Poll-Token", tt.header)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
