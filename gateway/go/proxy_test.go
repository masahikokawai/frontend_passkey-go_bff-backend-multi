package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGateway_resolveTarget(t *testing.T) {
	gw := &Gateway{Targets: map[string]string{"go": "http://localhost:8097"}}

	tests := []struct {
		name         string
		requested    string
		wantResolved string
		wantBaseURL  string
		wantFellBack bool
	}{
		{name: "実装済みのgoはそのまま使われる", requested: "go", wantResolved: "go", wantBaseURL: "http://localhost:8097", wantFellBack: false},
		{name: "未実装のrustはgoへフォールバックする", requested: "rust", wantResolved: "go", wantBaseURL: "http://localhost:8097", wantFellBack: true},
		{name: "未実装のscala-http4sはgoへフォールバックする", requested: "scala-http4s", wantResolved: "go", wantBaseURL: "http://localhost:8097", wantFellBack: true},
		{name: "未実装のscala-pekkoはgoへフォールバックする", requested: "scala-pekko", wantResolved: "go", wantBaseURL: "http://localhost:8097", wantFellBack: true},
		{name: "未実装のrailsはgoへフォールバックする", requested: "rails", wantResolved: "go", wantBaseURL: "http://localhost:8097", wantFellBack: true},
		{name: "未知の値でもgoへフォールバックする", requested: "cobol", wantResolved: "go", wantBaseURL: "http://localhost:8097", wantFellBack: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, base, fellBack := gw.resolveTarget(tt.requested)
			if resolved != tt.wantResolved {
				t.Errorf("resolved = %q, want %q", resolved, tt.wantResolved)
			}
			if base != tt.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", base, tt.wantBaseURL)
			}
			if fellBack != tt.wantFellBack {
				t.Errorf("fellBack = %v, want %v", fellBack, tt.wantFellBack)
			}
		})
	}
}

// TestGateway_Handler_ProxiesToResolvedTarget はhttptestで偽の上流サーバーを立て、
// Gatewayが実際にリクエスト(パス・クエリ・Authorizationヘッダ)を転送し、
// 上流のレスポンスをそのまま返すことを確認する
func TestGateway_Handler_ProxiesToResolvedTarget(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tasks":[]}`))
	}))
	defer upstream.Close()

	gw := &Gateway{
		Targets:         map[string]string{"go": upstream.URL},
		ResolveLanguage: func() string { return "go" },
	}

	gateway := httptest.NewServer(gw.Handler())
	defer gateway.Close()

	req, err := http.NewRequest(http.MethodGet, gateway.URL+"/external/v1/tasks?user_id=1", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != `{"tasks":[]}` {
		t.Errorf("body = %q, want %q", body, `{"tasks":[]}`)
	}
	if gotPath != "/external/v1/tasks" {
		t.Errorf("upstreamが受け取ったpath = %q, want /external/v1/tasks", gotPath)
	}
	if gotQuery != "user_id=1" {
		t.Errorf("upstreamが受け取ったquery = %q, want user_id=1", gotQuery)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("upstreamが受け取ったAuthorizationヘッダ = %q, want %q", gotAuth, "Bearer test-token")
	}
}

// TestGateway_Handler_FallsBackToGoForUnimplementedLanguage は、
// backend.task-languageが未実装言語(rust等)を指していても、実際にgo側(唯一実装済み)へ
// 転送されることを確認する(CONTRACT.mdセクション20.7のフォールバック仕様)
func TestGateway_Handler_FallsBackToGoForUnimplementedLanguage(t *testing.T) {
	var hit bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	gw := &Gateway{
		Targets:         map[string]string{"go": upstream.URL},
		ResolveLanguage: func() string { return "rust" }, // 未実装
	}

	gateway := httptest.NewServer(gw.Handler())
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/external/v1/tasks?user_id=1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200(goへフォールバックして応答が返るはず)", resp.StatusCode)
	}
	if !hit {
		t.Error("goのupstreamが呼ばれなかった(フォールバックが機能していない)")
	}
}

// 【テスト監査で発見・追記】gateway/goには元々CORSの仕組みが一切無く、swagger-uiの
// 「Try it out」機能(ブラウザから直接:8081へfetchする)が実際のブラウザ上でだけ
// CORSエラーで失敗する状態だった(curlでの動作確認では気づけない壊れ方)
func TestGateway_Handler_CORS_許可オリジンからの実リクエストにAllow系ヘッダが付く(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	gw := &Gateway{
		Targets:         map[string]string{"go": upstream.URL},
		ResolveLanguage: func() string { return "go" },
		AllowedOrigin:   "http://localhost:18080",
	}
	gateway := httptest.NewServer(gw.Handler())
	defer gateway.Close()

	req, _ := http.NewRequest(http.MethodGet, gateway.URL+"/external/v1/tasks", nil)
	req.Header.Set("Origin", "http://localhost:18080")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:18080" {
		t.Errorf("Access-Control-Allow-Origin = %q, want http://localhost:18080", got)
	}
}

func TestGateway_Handler_CORS_許可していないオリジンにはAllow_Originが付かない(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	gw := &Gateway{
		Targets:         map[string]string{"go": upstream.URL},
		ResolveLanguage: func() string { return "go" },
		AllowedOrigin:   "http://localhost:18080",
	}
	gateway := httptest.NewServer(gw.Handler())
	defer gateway.Close()

	req, _ := http.NewRequest(http.MethodGet, gateway.URL+"/external/v1/tasks", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty(未許可オリジン)", got)
	}
}

// プリフライト(OPTIONS)は上流(backend外部API)へ転送してはいけない。backend外部APIの
// ルーターはOPTIONSメソッドのルートを持たないため、転送すると404/405になり
// プリフライト自体が失敗する(実際のブラウザだけが壊れる、curlでは気づけない典型例)
func TestGateway_Handler_CORS_プリフライトは上流へ転送せずこの層で204を返す(t *testing.T) {
	var upstreamHit bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	gw := &Gateway{
		Targets:         map[string]string{"go": upstream.URL},
		ResolveLanguage: func() string { return "go" },
		AllowedOrigin:   "http://localhost:18080",
	}
	gateway := httptest.NewServer(gw.Handler())
	defer gateway.Close()

	req, _ := http.NewRequest(http.MethodOptions, gateway.URL+"/external/v1/tasks", nil)
	req.Header.Set("Origin", "http://localhost:18080")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:18080" {
		t.Errorf("Access-Control-Allow-Origin = %q, want http://localhost:18080", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Access-Control-Allow-Headersが空(Authorizationヘッダを使う実リクエストのプリフライトが通らなくなる)")
	}
	if upstreamHit {
		t.Error("プリフライトが上流(backend外部API)へ転送されてしまっている(OPTIONSルートが無いため実ブラウザでは失敗するはず)")
	}
}

// AllowedOriginが未設定(空文字列)の場合、CORSヘッダを一切付与しない後方互換の確認
// (既存のTestGateway_Handler_ProxiesToResolvedTarget等、AllowedOriginを設定しない
// テストがCORSヘッダの有無に依存せず動き続けることを明示的に確認する)
func TestGateway_Handler_CORS_AllowedOrigin未設定ならヘッダを付与しない(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	gw := &Gateway{
		Targets:         map[string]string{"go": upstream.URL},
		ResolveLanguage: func() string { return "go" },
	}
	gateway := httptest.NewServer(gw.Handler())
	defer gateway.Close()

	req, _ := http.NewRequest(http.MethodGet, gateway.URL+"/external/v1/tasks", nil)
	req.Header.Set("Origin", "http://localhost:18080")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}
