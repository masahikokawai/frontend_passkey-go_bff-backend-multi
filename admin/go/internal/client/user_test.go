package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/client"
)

const testAdminInternalToken = "test-admin-internal-token"

func newFakeBackend(t *testing.T, handlerFunc http.HandlerFunc) *client.UserClient {
	t.Helper()
	srv := httptest.NewServer(handlerFunc)
	t.Cleanup(srv.Close)
	return client.NewUserClient(srv.URL, testAdminInternalToken)
}

func TestUserClient_List_SendsAdminInternalTokenAndParsesUsers(t *testing.T) {
	var gotToken string
	c := newFakeBackend(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Admin-Internal-Token")
		if r.Method != http.MethodGet || r.URL.Path != "/internal/v1/admin/users" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"users": []map[string]any{
				{"id": 1, "name": "太郎", "email": "taro@example.com", "role": "general"},
			},
		})
	})

	users, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if diff := cmp.Diff(testAdminInternalToken, gotToken); diff != "" {
		t.Errorf("X-Admin-Internal-Token mismatch (-want +got):\n%s", diff)
	}
	want := []client.User{{ID: 1, Name: "太郎", Email: "taro@example.com", Role: "general"}}
	if diff := cmp.Diff(want, users); diff != "" {
		t.Errorf("List() mismatch (-want +got):\n%s", diff)
	}
}

func TestUserClient_Create_Success(t *testing.T) {
	c := newFakeBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/admin/users" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "new@example.com" {
			t.Errorf("email = %q, want new@example.com", body["email"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_id": 5, "name": "新規", "email": "new@example.com", "role": "general",
		})
	})

	user, err := c.Create(context.Background(), client.CreateInput{
		Name: "新規", Email: "new@example.com", Password: "password", Role: "general",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	want := client.User{ID: 5, Name: "新規", Email: "new@example.com", Role: "general"}
	if diff := cmp.Diff(want, user); diff != "" {
		t.Errorf("Create() mismatch (-want +got):\n%s", diff)
	}
}

func TestUserClient_Create_ValidationError(t *testing.T) {
	c := newFakeBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "email is already taken"})
	})

	_, err := c.Create(context.Background(), client.CreateInput{Name: "x", Email: "dup@example.com", Password: "p", Role: "general"})
	if !errors.Is(err, client.ErrValidation) {
		t.Errorf("error = %v, want wrapping ErrValidation", err)
	}
}

func TestUserClient_UpdateRole(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		errorKey   string
		wantErr    error
		wantNilErr bool
	}{
		{name: "成功", status: http.StatusNoContent, wantNilErr: true},
		{name: "最後の管理者ガード", status: http.StatusUnprocessableEntity, errorKey: "last_manager_user", wantErr: client.ErrLastManager},
		{name: "存在しない", status: http.StatusNotFound, wantErr: client.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newFakeBackend(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/internal/v1/admin/users/7/role" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if tt.errorKey != "" {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tt.status)
				if tt.errorKey != "" {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": tt.errorKey})
				}
			})
			err := c.UpdateRole(context.Background(), 7, "management")
			if tt.wantNilErr {
				if err != nil {
					t.Errorf("UpdateRole() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("UpdateRole() error = %v, want wrapping %v", err, tt.wantErr)
			}
		})
	}
}

func TestUserClient_Delete(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		errorKey   string
		wantErr    error
		wantNilErr bool
	}{
		{name: "成功", status: http.StatusNoContent, wantNilErr: true},
		{name: "最後の管理者ガード", status: http.StatusUnprocessableEntity, errorKey: "last_manager_user", wantErr: client.ErrLastManager},
		{name: "存在しない", status: http.StatusNotFound, wantErr: client.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newFakeBackend(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != "/internal/v1/admin/users/9" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if tt.errorKey != "" {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tt.status)
				if tt.errorKey != "" {
					_ = json.NewEncoder(w).Encode(map[string]string{"error": tt.errorKey})
				}
			})
			err := c.Delete(context.Background(), 9)
			if tt.wantNilErr {
				if err != nil {
					t.Errorf("Delete() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Delete() error = %v, want wrapping %v", err, tt.wantErr)
			}
		})
	}
}
