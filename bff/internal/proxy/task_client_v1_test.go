package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/masahikokawai/bff-gin/bff/internal/auth"
)

// TestTaskClientV1_Create_SendsSnakeCaseFields は実機デバッグで見つかった不具合の
// 回帰テスト: backendはスネークケース(finished_on, label_ids)のJSONを期待しているが、
// 以前はcamelCase(finishedOn, labelIds)で送っておりGinのbindingが失敗し400になっていた
func TestTaskClientV1_Create_SendsSnakeCaseFields(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("リクエストボディのデコードに失敗: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"test","status":"waiting","finished_on":"2030-01-01","labels":[],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewTaskClientV1(server.URL)
	_, err := client.Create(context.Background(), "token", 1, TaskInput{
		Name:       "test",
		Status:     "waiting",
		FinishedOn: "2030-01-01",
		LabelIDs:   []uint64{1, 2},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, ok := gotBody["finished_on"]; !ok {
		t.Errorf("リクエストボディにfinished_onが無い: %v", gotBody)
	}
	if _, ok := gotBody["label_ids"]; !ok {
		t.Errorf("リクエストボディにlabel_idsが無い: %v", gotBody)
	}
	if _, ok := gotBody["finishedOn"]; ok {
		t.Errorf("リクエストボディにcamelCaseのfinishedOnが含まれるべきではない: %v", gotBody)
	}
	if _, ok := gotBody["labelIds"]; ok {
		t.Errorf("リクエストボディにcamelCaseのlabelIdsが含まれるべきではない: %v", gotBody)
	}
}

func TestTaskClientV1_Create_ParsesSnakeCaseResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"test","status":"waiting","finished_on":"2030-01-01","labels":[{"id":9,"name":"重要"}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewTaskClientV1(server.URL)
	task, err := client.Create(context.Background(), "token", 1, TaskInput{Name: "test", Status: "waiting", FinishedOn: "2030-01-01"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.FinishedOn != "2030-01-01" {
		t.Errorf("FinishedOn = %q, want 2030-01-01(finished_onが正しくパースされていない)", task.FinishedOn)
	}
	if len(task.Labels) != 1 || task.Labels[0].Name != "重要" {
		t.Errorf("Labels = %v, want [{9 重要}]", task.Labels)
	}
}

func TestTaskClientV1_List_UpstreamUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewTaskClientV1(server.URL)
	_, err := client.List(context.Background(), "expired-token", 1, TaskFilter{Limit: 20})
	if !errors.Is(err, auth.ErrUpstreamUnauthorized) {
		t.Errorf("List() error = %v, want ErrUpstreamUnauthorized", err)
	}
}

// 実機検証で発覚した不具合の回帰テスト:
// admin 画面でログイン中のユーザーを削除すると、backend は403 {"error":"user_not_provisioned"} を返す
// 以前はこれが一般エラーとして扱われ、生のエラー文字列がそのままフロントへ表示されていた
// auth.ErrUserNotProvisioned として分類されることを確認する(この後Refresher.Doがセッションを破棄し401にする)
func TestTaskClientV1_List_UserNotProvisioned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"user_not_provisioned"}`))
	}))
	defer server.Close()

	client := NewTaskClientV1(server.URL)
	_, err := client.List(context.Background(), "token-of-deleted-user", 1, TaskFilter{Limit: 20})
	if !errors.Is(err, auth.ErrUserNotProvisioned) {
		t.Errorf("List() error = %v, want ErrUserNotProvisioned", err)
	}
}

// 403だが別の理由(例: 権限不足)の場合は、ErrUserNotProvisionedへ誤分類しないことを確認する
func TestTaskClientV1_List_ForbiddenOtherReason_NotClassifiedAsUserNotProvisioned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	client := NewTaskClientV1(server.URL)
	_, err := client.List(context.Background(), "token", 1, TaskFilter{Limit: 20})
	if errors.Is(err, auth.ErrUserNotProvisioned) {
		t.Errorf("List() error = %v, should not be classified as ErrUserNotProvisioned", err)
	}
	if err == nil {
		t.Fatal("List() error = nil, want an error")
	}
}

// テストコード監査(2回目)で見つかった穴の解消:
// classifyStatus の err! = nil 分岐
// (backend自体に接続できない場合)がこれまで一度も実際のネットワーク断で検証されて
// いなかった(スタブ化されたHTTPステータスのテストしかなかった)
// 接続を一切 listen していないポートへ向けることで、実際にconnection refusedを発生させる
func TestTaskClientV1_List_ConnectionRefused_ReturnsGenericErrorNotMisclassified(t *testing.T) {
	// 一度listenしてすぐcloseしたポートは高確率で誰も使っていない状態になる
	// (別プロセスが同じ瞬間に同じポートを奪う可能性はゼロではないが、テスト用途としては十分)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	closedAddr := ln.Addr().String()
	ln.Close()

	client := NewTaskClientV1("http://" + closedAddr)
	_, err = client.List(context.Background(), "token", 1, TaskFilter{Limit: 20})
	if err == nil {
		t.Fatal("List() error = nil, backendに接続できないのでエラーを期待した")
	}
	// 接続断はHTTPステータスを伴わないため、401/403系の分類には絶対に
	// 入ってはいけない(誤ってErrUpstreamUnauthorizedになると、Refresher.Doが
	// 無駄なトークンリフレッシュを試みてしまう)
	if errors.Is(err, auth.ErrUpstreamUnauthorized) || errors.Is(err, auth.ErrUserNotProvisioned) {
		t.Errorf("List() error = %v, 接続断が401/403系に誤分類されている", err)
	}
}

// backendが200を返しつつ、ボディがTaskListResultとして解釈できない不正なJSONだった場合、
// 「エラーには気づかず空/ゼロ値のTaskListResultをそのまま返してしまう」という
// サイレントな不具合が無いかを確認する(resty.SetResultは内部でUnmarshalしているが、
// その失敗がclient.List()の戻り値のerrorとしてちゃんと伝播するかは自明ではないため、
// 実際のレスポンスで検証する)
func TestTaskClientV1_List_MalformedJSONBody_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tasks": [`)) // 意図的に壊れたJSON(閉じ括弧が無い)
	}))
	defer server.Close()

	client := NewTaskClientV1(server.URL)
	_, err := client.List(context.Background(), "token", 1, TaskFilter{Limit: 20})
	if err == nil {
		t.Error("List() error = nil, 不正なJSONボディなのでエラーを期待した(サイレントに空データを返すのは事故のもと)")
	}
}

func TestTaskClientV1_Delete_SendsAuthTokenAndUserID(t *testing.T) {
	var gotAuthHeader, gotUserID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotUserID = r.URL.Query().Get("user_id")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewTaskClientV1(server.URL)
	if err := client.Delete(context.Background(), "my-token", 7, 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if gotAuthHeader != "Bearer my-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuthHeader, "Bearer my-token")
	}
	if gotUserID != "7" {
		t.Errorf("user_id query param = %q, want 7", gotUserID)
	}
}
