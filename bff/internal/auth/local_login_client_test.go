package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestLocalLoginClient_VerifyPassword_Success はbackendの
// POST /internal/v1/auth/verify-local-password が200を返した場合、
// レスポンスのuser_id/name/email/rolesをそのまま返すことを確認する
func TestLocalLoginClient_VerifyPassword_Success(t *testing.T) {
	var gotHeader string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Local-Auth-Internal-Token")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("リクエストボディのデコードに失敗: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_id":1,"name":"ローカル太郎","email":"local-user@example.com","roles":["general"]}`))
	}))
	defer server.Close()

	client := NewLocalLoginClient(server.URL, "internal-token")
	userID, name, email, roles, err := client.VerifyPassword(context.Background(), "local-user@example.com", "password")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}

	if gotHeader != "internal-token" {
		t.Errorf("X-Local-Auth-Internal-Tokenヘッダ = %q, want internal-token", gotHeader)
	}
	if diff := cmp.Diff(map[string]string{"email": "local-user@example.com", "password": "password"}, gotBody); diff != "" {
		t.Errorf("リクエストボディが期待と異なる(-want +got):\n%s", diff)
	}
	if userID != 1 || name != "ローカル太郎" || email != "local-user@example.com" || len(roles) != 1 || roles[0] != "general" {
		t.Errorf("got userID=%d name=%q email=%q roles=%v", userID, name, email, roles)
	}
}

// TestLocalLoginClient_VerifyPassword_InvalidCredentials はemail不一致/bcrypt不一致時、
// 401 {"error":"invalid_credentials"} を ErrInvalidLocalCredentials に変換することを確認する
func TestLocalLoginClient_VerifyPassword_InvalidCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_credentials"}`))
	}))
	defer server.Close()

	client := NewLocalLoginClient(server.URL, "internal-token")
	_, _, _, _, err := client.VerifyPassword(context.Background(), "x@example.com", "wrong")
	if !errors.Is(err, ErrInvalidLocalCredentials) {
		t.Errorf("err = %v, want ErrInvalidLocalCredentials", err)
	}
}

// TestLocalLoginClient_VerifyPassword_PasswordExpired はCONTRACT.mdセクション16.3の
// 「有効期限切れは資格情報不一致と区別する」設計が、bff側の実HTTPクライアントの
// レスポンス解釈まで一貫していることを確認する回帰テスト
func TestLocalLoginClient_VerifyPassword_PasswordExpired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"password_expired"}`))
	}))
	defer server.Close()

	client := NewLocalLoginClient(server.URL, "internal-token")
	_, _, _, _, err := client.VerifyPassword(context.Background(), "local-user@example.com", "password")
	if !errors.Is(err, ErrLocalPasswordExpired) {
		t.Errorf("err = %v, want ErrLocalPasswordExpired", err)
	}
}

// TestLocalLoginClient_VerifyPassword_UnexpectedResponse はerrorキーが未知の値
// (実装ミス・backend側の仕様変更等)の場合、黙って成功扱いにせずエラーにすることを確認する
func TestLocalLoginClient_VerifyPassword_UnexpectedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal_server_error"}`))
	}))
	defer server.Close()

	client := NewLocalLoginClient(server.URL, "internal-token")
	_, _, _, _, err := client.VerifyPassword(context.Background(), "local-user@example.com", "password")
	if err == nil {
		t.Fatal("エラーになるべきだが成功した")
	}
	if errors.Is(err, ErrInvalidLocalCredentials) || errors.Is(err, ErrLocalPasswordExpired) {
		t.Errorf("未知のエラー種別をinvalid_credentials/password_expiredに丸めるべきではない: %v", err)
	}
}
