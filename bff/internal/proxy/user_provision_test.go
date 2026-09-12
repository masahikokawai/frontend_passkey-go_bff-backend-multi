package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUserProvisionClient_Provision_SendsBearerToken は実機デバッグで見つかった
// 不具合の回帰テスト: 当初このクライアントはAuthorizationヘッダを一切送っておらず、
// backend側のRequireAuthミドルウェアで401になっていた
// (「ネットワーク分離だけで認可を代替する」という誤った前提だった)
func TestUserProvisionClient_Provision_SendsBearerToken(t *testing.T) {
	var gotAuthHeader, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_id": 42, "role": "general"}`))
	}))
	defer server.Close()

	client := NewUserProvisionClient(server.URL)
	userID, role, err := client.Provision(context.Background(), "test-access-token", "sub-1", "太郎", "taro@example.com", []string{"general"})
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	wantPath := "/internal/v1/users/provision"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}

	wantAuth := "Bearer test-access-token"
	if gotAuthHeader != wantAuth {
		t.Errorf("Authorization header = %q, want %q(以前はヘッダ自体が送られておらず401になっていた)", gotAuthHeader, wantAuth)
	}

	if userID != 42 {
		t.Errorf("userID = %d, want 42", userID)
	}
	if role != "general" {
		t.Errorf("role = %q, want general", role)
	}
}

func TestUserProvisionClient_Provision_SendsRequestBodyFields(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_id": 1, "role": "general"}`))
	}))
	defer server.Close()

	client := NewUserProvisionClient(server.URL)
	if _, _, err := client.Provision(context.Background(), "token", "sub-99", "花子", "hanako@example.com", []string{"management"}); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	if gotBody["keycloak_sub"] != "sub-99" {
		t.Errorf("keycloak_sub = %v, want sub-99", gotBody["keycloak_sub"])
	}
	if gotBody["name"] != "花子" {
		t.Errorf("name = %v, want 花子", gotBody["name"])
	}
	if gotBody["email"] != "hanako@example.com" {
		t.Errorf("email = %v, want hanako@example.com", gotBody["email"])
	}
}

func TestUserProvisionClient_Provision_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer server.Close()

	client := NewUserProvisionClient(server.URL)
	if _, _, err := client.Provision(context.Background(), "bad-token", "sub-1", "太郎", "taro@example.com", nil); err == nil {
		t.Fatal("Provision() with 401 response should return an error")
	}
}
