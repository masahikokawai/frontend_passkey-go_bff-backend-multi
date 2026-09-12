package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 【テスト監査で発見】label_route.goには既存テストが1件も無かった。TaskRoutesと同様、
// Create/UpdateがShouldBindJSONの失敗を確実に400へマッピングし、panicしないことを
// まず最小限のカバレッジとして固定する(newContextWithSessionはtask_route_test.goで定義済み、
// 同一パッケージのため共有できる)

func TestLabelRoutes_Create_不正なJSONボディは400になりpanicしない(t *testing.T) {
	routes := &LabelRoutes{}
	req := httptest.NewRequest(http.MethodPost, "/api/labels", strings.NewReader(`{not valid json truncated`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c := newContextWithSession(w, req)

	routes.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestLabelRoutes_Create_空ボディは400になりpanicしない(t *testing.T) {
	routes := &LabelRoutes{}
	req := httptest.NewRequest(http.MethodPost, "/api/labels", strings.NewReader(``))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c := newContextWithSession(w, req)

	routes.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}
