package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLabelClientV1_Create(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"重要"}`))
	}))
	defer server.Close()

	client := NewLabelClientV1(server.URL)
	got, err := client.Create(context.Background(), "token", "重要")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/internal/v1/labels" {
		t.Errorf("path = %s, want /internal/v1/labels", gotPath)
	}
	if diff := cmp.Diff(map[string]any{"name": "重要"}, gotBody); diff != "" {
		t.Errorf("request body mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(Label{ID: 1, Name: "重要"}, got); diff != "" {
		t.Errorf("result mismatch (-want +got):\n%s", diff)
	}
}

func TestLabelClientV1_Update(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"改名"}`))
	}))
	defer server.Close()

	client := NewLabelClientV1(server.URL)
	got, err := client.Update(context.Background(), "token", 1, "改名")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/internal/v1/labels/1" {
		t.Errorf("path = %s, want /internal/v1/labels/1", gotPath)
	}
	if got.Name != "改名" {
		t.Errorf("Name = %s, want 改名", got.Name)
	}
}

func TestLabelClientV1_Delete(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewLabelClientV1(server.URL)
	if err := client.Delete(context.Background(), "token", 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/internal/v1/labels/1" {
		t.Errorf("path = %s, want /internal/v1/labels/1", gotPath)
	}
}

func TestLabelClientV1_Create_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewLabelClientV1(server.URL)
	_, err := client.Create(context.Background(), "expired-token", "x")
	if err == nil {
		t.Fatal("エラーを期待したがnilだった")
	}
}
