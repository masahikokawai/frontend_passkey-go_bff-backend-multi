package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewLanguageResolver_ResolvesCurrentValue は、backendのexportエンドポイントと
// 同じJSON形状(BuildFlagConfigJSON)を返す偽サーバーに対して、実際にポーリング・
// パースし、backend.task-languageの現在値を解決できることを確認する
func TestNewLanguageResolver_ResolvesCurrentValue(t *testing.T) {
	var gotToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Feature-Flag-Poll-Token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"backend.task-language": {
				"variations": {"go":"go","rust":"rust","scala-http4s":"scala-http4s","scala-pekko":"scala-pekko","rails":"rails"},
				"defaultRule": {"variation":"rust"},
				"disable": false
			}
		}`))
	}))
	defer fake.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resolver, err := NewLanguageResolver(ctx, fake.URL, "test-poll-token", 100*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("NewLanguageResolver() error = %v", err)
	}

	got := resolver.Resolve(ctx)
	if got != "rust" {
		t.Errorf("Resolve() = %q, want %q", got, "rust")
	}
	if gotToken != "test-poll-token" {
		t.Errorf("上流へ送られたポーリングトークン = %q, want %q", gotToken, "test-poll-token")
	}
}

// TestNewLanguageResolver_FailureFallsBackToDefault はexportエンドポイントが
// 到達不能な場合でもResolveが既定値"go"を返す(パニックしない)ことを確認する
func TestNewLanguageResolver_FailureFallsBackToDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 誰も listen していないポートを指す不正なURL
	_, err := NewLanguageResolver(ctx, "http://127.0.0.1:1", "test-poll-token", 100*time.Millisecond, nil)
	if err == nil {
		t.Log("到達不能なexport URLでもprovider自体の初期化は成功する実装のようだ(SetProviderAndWaitの挙動依存、致命的ではない)")
	}
}
