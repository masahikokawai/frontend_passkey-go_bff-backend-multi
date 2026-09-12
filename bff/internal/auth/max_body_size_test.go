package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newTestGinRouterForMaxBodySize はMaxBodySizeMiddleware単体を検証するための最小ルーター
// (Task/Labelハンドラと同じ「ShouldBindJSONのエラーを400へ」パターンをこの場で再現する)
func newTestGinRouterForMaxBodySize(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(MaxBodySizeMiddleware())
	router.POST("/echo", func(c *gin.Context) {
		var body struct {
			Data string `json:"data"`
			OK   bool   `json:"ok"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"received": true})
	})
	return router
}

func TestMaxBodySizeMiddleware_上限以内のボディはそのまま読める(t *testing.T) {
	gin := newTestGinRouterForMaxBodySize(t)

	body := strings.NewReader(`{"ok":true}`)
	req := httptest.NewRequest(http.MethodPost, "/echo", body)
	w := httptest.NewRecorder()
	gin.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
}

// 【テスト監査で追加】上限(1MiB)を超えるボディを送ると、読み取り自体がエラーになり、
// 呼び出し側(ShouldBindJSON)が既存のエラーハンドリングでこれを400として扱えることを確認する
// (http.MaxBytesReaderはRead時にエラーを返すだけで、書き込み側のResponseWriterに
// 直接ステータスを書き込むわけではないため、既存の「ShouldBindJSONのエラーを400へ」という
// 各ハンドラのパターンがそのまま機能する)
func TestMaxBodySizeMiddleware_上限を超えるボディは読み取り時にエラーになる(t *testing.T) {
	gin := newTestGinRouterForMaxBodySize(t)

	oversized := strings.Repeat("a", maxRequestBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"data":"`+oversized+`"}`))
	w := httptest.NewRecorder()
	gin.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400(ShouldBindJSONがMaxBytesReaderのエラーを検知して返す), body=%s", w.Code, w.Body.String())
	}
}
