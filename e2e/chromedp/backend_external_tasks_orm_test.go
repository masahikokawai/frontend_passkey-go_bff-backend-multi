package e2e

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
)

// setExternalTasksOrm はMySQLへ直接接続し、多値Feature Flag `backend.external-tasks-orm`
// (CONTRACT.mdセクション24)の default_variation を書き換える。setTaskLanguage
// (backend_task_language_test.go)と全く同じ方式・同じ「テスト終了時に必ず元へ戻す」規約に従う。
func setExternalTasksOrm(t *testing.T, variation string) {
	t.Helper()

	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil {
		t.Fatalf("MySQL接続に失敗しました: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var before string
	if err := db.QueryRow(`SELECT default_variation FROM feature_flags WHERE flag_key = 'backend.external-tasks-orm'`).Scan(&before); err != nil {
		t.Fatalf("backend.external-tasks-ormの現在値取得に失敗しました: %v", err)
	}

	if _, err := db.Exec(`UPDATE feature_flags SET default_variation = ? WHERE flag_key = 'backend.external-tasks-orm'`, variation); err != nil {
		t.Fatalf("backend.external-tasks-ormの更新に失敗しました: %v", err)
	}

	t.Cleanup(func() {
		if _, err := db.Exec(`UPDATE feature_flags SET default_variation = ? WHERE flag_key = 'backend.external-tasks-orm'`, before); err != nil {
			t.Errorf("backend.external-tasks-ormを元の値(%s)へ戻すのに失敗しました: %v", before, err)
		}
	})

	waitForFeatureFlagPropagation()
}

// fetchExternalAPIToken はKeycloakのClient Credentials Grantで外部公開API用の
// access_tokenを取得する(README.mdの「外部公開API」セクションのcurl手順と全く同じ)。
// backend自身がこのFeature Flagを直接評価する経路(bffのポーリングを経由しない)なので、
// このテストではbffもfrontendも一切使わない
func fetchExternalAPIToken(t *testing.T) string {
	t.Helper()

	tokenURL := envOr("KEYCLOAK_TOKEN_URL", "http://localhost:8082/realms/training/protocol/openid-connect/token")
	resp, err := http.PostForm(tokenURL, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {envOr("EXTERNAL_API_CLIENT_ID", "external-api-client")},
		"client_secret": {envOr("EXTERNAL_API_CLIENT_SECRET", "external-api-client-local-dev-secret")},
	})
	if err != nil {
		t.Fatalf("Keycloakのトークンエンドポイントへのリクエストに失敗しました: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("トークンレスポンスの読み取りに失敗しました: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("トークン取得が失敗しました: status=%d body=%s", resp.StatusCode, body)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("トークンレスポンスのJSONパースに失敗しました: %v body=%s", err, body)
	}
	if parsed.AccessToken == "" {
		t.Fatalf("access_tokenが空です: body=%s", body)
	}
	return parsed.AccessToken
}

// externalTasksBaseURL はbackend自身が待ち受ける外部公開APIの内部アドレス(既定:8097)。
// gateway(:8081、CONTRACT.mdセクション20.7〜20.8)は「追加構成」で既定では起動していないため、
// gateway未起動でも確認できるようbackendへ直接向ける(README.mdに明記された確認方法と同じ)
func externalTasksBaseURL() string {
	return envOr("BACKEND_EXTERNAL_API_BASE_URL", "http://localhost:8097")
}

// TestExternalTasksAPI_BackendExternalTasksOrmBob はCONTRACT.mdセクション24
// (外部公開APIのTask一覧取得をGORM/bobのどちらで動かすか切り替えるFeature Flag)の
// 「bobに切り替えても、GORMと完全に同一のレスポンス形状で応答する」ことを検証する。
// これまでランブックの手順に沿ったcurl+目視でのみ確認されており、自動テストが1つも
// 無かった(e2e/以下でexternal-tasks-orm/external_tasks_ormを検索してもヒット無し)。
//
// backend.task-languageのテスト(backend_task_language_test.go)と異なり、これは
// backend(Go、コア構成として既定で起動している)自身が評価する軸で、bff・frontendを
// 一切経由しないため、ブラウザ操作は不要な純粋なHTTPレベルのテストにしている
func TestExternalTasksAPI_BackendExternalTasksOrmBob(t *testing.T) {
	setExternalTasksOrm(t, "bob")
	token := fetchExternalAPIToken(t)

	req, err := http.NewRequest(http.MethodGet, externalTasksBaseURL()+"/external/v1/tasks?user_id=1", nil)
	if err != nil {
		t.Fatalf("リクエストの組み立てに失敗しました: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /external/v1/tasks(backend.external-tasks-orm=bob)に失敗しました: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("レスポンスの読み取りに失敗しました: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", resp.StatusCode, body)
	}

	// レスポンス形状はGORM実装と完全に同一のはず(CONTRACT.mdセクション24、
	// backend.external-tasks-pagination-v2=falseの既定ではoffsetページング形状)
	var parsed struct {
		Tasks    []map[string]any `json:"tasks"`
		Page     float64          `json:"page"`
		PageSize float64          `json:"page_size"`
		Total    float64          `json:"total"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("レスポンスのJSONパースに失敗しました: %v body=%s", err, body)
	}
	if parsed.Tasks == nil {
		t.Errorf("tasksフィールドが無い(または配列でない): body=%s", body)
	}
	if parsed.Page != 1 {
		t.Errorf("page = %v, want 1 (既定値)", parsed.Page)
	}
	if parsed.PageSize != 10 {
		t.Errorf("page_size = %v, want 10 (既定値)", parsed.PageSize)
	}
}
