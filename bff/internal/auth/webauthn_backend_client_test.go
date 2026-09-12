package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// decodeJSONBody はリクエストボディをJSONとしてoutへデコードするテスト用ヘルパー
// (このファイル内で複数回使うため関数化している)
func decodeJSONBody(t *testing.T, r *http.Request, out *map[string]any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatalf("リクエストボディのJSONデコードに失敗: %v", err)
	}
}

// このファイルはCONTRACT.mdセクション22.4の3エンドポイントを呼ぶHTTPクライアント層のテスト
// テスト監査で判明: webauthn_backend_client.go には元々テストが1件も無かった
// (proxyパッケージの他のHTTPクライアント(TaskClientV1・LabelClientV1等)には
// 必ずhttptest.Serverを使ったテストがある、という既存の慣例から外れていた)
//
// 特にステータスコード分岐(201/404/その他)は、bffの正否判定に直結するため
// (404を見誤ると「未登録のパスキー」と「backend側の予期しないエラー」を
// 区別できなくなる)、最低限このステータスコード分岐は網羅する
func TestWebauthnBackendClient_Register(t *testing.T) {
	t.Run("201なら成功", func(t *testing.T) {
		var gotAuth, gotPath string
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			gotPath = r.URL.Path
			decodeJSONBody(t, r, &gotBody)
			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		client := NewWebauthnBackendClient(server.URL, "unused-for-register")
		err := client.Register(context.Background(), "user-access-token", "cred-id", "pub-key", 3, true, false, []string{"internal", "hybrid"}, "MacBook")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		if gotAuth != "Bearer user-access-token" {
			t.Errorf("Authorization header = %q, want Bearer user-access-token(登録はユーザー自身のaccess tokenを転送する設計、共有シークレットではない)", gotAuth)
		}
		if gotPath != "/internal/v1/auth/webauthn/credentials" {
			t.Errorf("path = %s", gotPath)
		}
		if gotBody["credential_id"] != "cred-id" || gotBody["public_key"] != "pub-key" || gotBody["name"] != "MacBook" {
			t.Errorf("request body = %v", gotBody)
		}
	})

	t.Run("201以外はエラーを返す(backend側のバリデーションエラー等を握りつぶさない)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"validation_error"}`))
		}))
		defer server.Close()

		client := NewWebauthnBackendClient(server.URL, "unused")
		err := client.Register(context.Background(), "token", "cred-id", "pub-key", 0, false, false, nil, "")
		if err == nil {
			t.Fatal("Register() error = nil, want non-nil")
		}
	})
}

func TestWebauthnBackendClient_Lookup(t *testing.T) {
	t.Run("200ならuser_id/name/email/roles/公開鍵/sign_countを返す", func(t *testing.T) {
		var gotHeader, gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeader = r.Header.Get("X-Webauthn-Internal-Token")
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"user_id":6,"name":"ローカル太郎","email":"local-user@example.com","roles":["general"],"public_key":"cGs=","sign_count":5,"backup_eligible":true,"backup_state":true}`))
		}))
		defer server.Close()

		client := NewWebauthnBackendClient(server.URL, "shared-secret")
		userID, name, email, roles, publicKey, signCount, backupEligible, backupState, err := client.Lookup(context.Background(), "cred-id")
		if err != nil {
			t.Fatalf("Lookup() error = %v", err)
		}
		if gotHeader != "shared-secret" {
			t.Errorf("X-Webauthn-Internal-Token = %q, want shared-secret(ログイン試行中はJWTが無いため共有シークレットで認可する設計、CONTRACT.mdセクション22.4)", gotHeader)
		}
		if gotPath != "/internal/v1/auth/webauthn/credentials/cred-id" {
			t.Errorf("path = %s", gotPath)
		}
		if userID != 6 || name != "ローカル太郎" || email != "local-user@example.com" || len(roles) != 1 || roles[0] != "general" || publicKey != "cGs=" || signCount != 5 {
			t.Errorf("got userID=%d name=%q email=%q roles=%v publicKey=%q signCount=%d", userID, name, email, roles, publicKey, signCount)
		}
		// 【実機デバッグで追記】BE/BSフラグが正しくパースされることを固定する
		// (これが抜けていたことがクラウド同期パスキーのログイン失敗の実際の原因だった)
		if !backupEligible || !backupState {
			t.Errorf("got backupEligible=%v backupState=%v, want true/true", backupEligible, backupState)
		}
	})

	t.Run("404はErrWebauthnCredentialNotFoundを返す(未登録credential_idと他のエラーを呼び出し元が区別できるように)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewWebauthnBackendClient(server.URL, "shared-secret")
		_, _, _, _, _, _, _, _, err := client.Lookup(context.Background(), "unknown-cred")
		if !errors.Is(err, ErrWebauthnCredentialNotFound) {
			t.Errorf("err = %v, want ErrWebauthnCredentialNotFound", err)
		}
	})

	t.Run("404以外の予期しないステータスはErrWebauthnCredentialNotFoundにしない(backend障害をパスキー未登録と誤認しないように)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewWebauthnBackendClient(server.URL, "shared-secret")
		_, _, _, _, _, _, _, _, err := client.Lookup(context.Background(), "cred-id")
		if err == nil {
			t.Fatal("Lookup() error = nil, want non-nil")
		}
		if errors.Is(err, ErrWebauthnCredentialNotFound) {
			t.Error("500エラーがErrWebauthnCredentialNotFoundとして扱われている(誤り)")
		}
	})
}

func TestWebauthnBackendClient_UpdateSignCount(t *testing.T) {
	t.Run("204なら成功", func(t *testing.T) {
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decodeJSONBody(t, r, &gotBody)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := NewWebauthnBackendClient(server.URL, "shared-secret")
		if err := client.UpdateSignCount(context.Background(), "cred-id", 42); err != nil {
			t.Fatalf("UpdateSignCount() error = %v", err)
		}
		if gotBody["sign_count"] != float64(42) {
			t.Errorf("request body sign_count = %v, want 42", gotBody["sign_count"])
		}
	})

	t.Run("204以外はエラーを返す", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewWebauthnBackendClient(server.URL, "shared-secret")
		if err := client.UpdateSignCount(context.Background(), "unknown-cred", 1); err == nil {
			t.Fatal("UpdateSignCount() error = nil, want non-nil")
		}
	})
}
