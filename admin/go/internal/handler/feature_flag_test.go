package handler_test

// 実際のGinルーター(router.goが組み立てるもの)にhttptestでリクエストを送る、
// 「簡易的なe2e」に近い位置づけのハンドラ層テスト
// admin/rails側のリクエストスペックと同じ観点を、Go側でも揃える
// testify不使用、google/go-cmpで比較する既存backend/bffの方針を踏襲する

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/client"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/handler"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/repository"
	"github.com/masahikokawai/training-go/bff-gin/admin-go/internal/service"
)

const (
	testBasicAuthUser     = "test-admin"
	testBasicAuthPassword = "test-password"
	testFlagKey           = "test.admin-go-handler-test"
)

func basicAuthHeader(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// setupTestRouter は実MySQL(TEST_DB_DSN)に接続し、テスト専用のフラグを1件
// クリーンな状態から作った上で、本番のcmd/server/main.goと同じ組み立て方をした
// *gin.Engineを返す
func setupTestRouter(t *testing.T) (*gorm.DB, http.Handler, *model.FeatureFlag) {
	t.Helper()

	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ")
	}

	gormDB, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}

	// テスト専用フラグをクリーンな状態から作る(冪等性のため既存分を削除してから作成)
	if err := gormDB.Unscoped().Where("flag_key = ?", testFlagKey).Delete(&model.FeatureFlag{}).Error; err != nil {
		t.Fatalf("既存テストフラグの削除に失敗: %v", err)
	}
	flag := model.FeatureFlag{
		FlagKey:          testFlagKey,
		DefaultVariation: "off",
		Enabled:          true,
	}
	if err := gormDB.Create(&flag).Error; err != nil {
		t.Fatalf("テストフラグ作成に失敗: %v", err)
	}
	t.Cleanup(func() {
		gormDB.Unscoped().Where("feature_flag_id = ?", flag.ID).Delete(&model.FeatureFlagAuditLog{})
		gormDB.Unscoped().Where("id = ?", flag.ID).Delete(&model.FeatureFlag{})
	})

	// テンプレートのglobは admin/go/ 直下からの相対パス想定(cmd/server/main.go参照)
	// go testはパッケージのソースディレクトリ(internal/handler/)をcwdにするため、
	// ここではそこからの相対パスにする
	handler.LoadTemplates("../../web/templates/*.html")

	repo := repository.NewFeatureFlag(gormDB)
	svc := service.NewFeatureFlagService(repo)
	h := handler.NewFeatureFlagHandler(svc)
	// このテストファイルはFeature Flag機能のみを対象とするため、UserHandlerは
	// 実際には呼ばれない(到達しないダミーのbaseURLで構わない)
	uh := handler.NewUserHandler(client.NewUserClient("http://unused.invalid", "unused"))
	router := handler.NewRouter(h, uh, testBasicAuthUser, testBasicAuthPassword)

	return gormDB, router, &flag
}

func doRequest(router http.Handler, method, target string, body url.Values, authHeader string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(body.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestFeatureFlagHandlers_RequireBasicAuth(t *testing.T) {
	_, router, flag := setupTestRouter(t)
	id := strconv.FormatUint(flag.ID, 10)

	targets := []string{
		"/",
		"/flags/" + id + "/edit",
		"/flags/" + id + "/audit_log",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			w := doRequest(router, http.MethodGet, target, nil, "")
			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d(Basic Auth無し)", w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestFeatureFlagHandlers_RequireBasicAuth_WrongCredentials(t *testing.T) {
	_, router, _ := setupTestRouter(t)

	w := doRequest(router, http.MethodGet, "/", nil, basicAuthHeader(testBasicAuthUser, "wrong-password"))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d(パスワード誤り)", w.Code, http.StatusUnauthorized)
	}
}

func TestFeatureFlagHandlers_Index_ListsFlags(t *testing.T) {
	_, router, flag := setupTestRouter(t)

	w := doRequest(router, http.MethodGet, "/", nil, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), flag.FlagKey) {
		t.Errorf("レスポンスボディに flag_key=%q が含まれていない", flag.FlagKey)
	}
}

func TestFeatureFlagHandlers_Edit_ShowsCurrentValue(t *testing.T) {
	_, router, flag := setupTestRouter(t)
	id := strconv.FormatUint(flag.ID, 10)

	w := doRequest(router, http.MethodGet, "/flags/"+id+"/edit", nil, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, flag.FlagKey) {
		t.Errorf("編集画面に flag_key=%q が表示されていない", flag.FlagKey)
	}
	// flag.DefaultVariation == "off" が初期値なので、offがselectedになっているはず
	if !strings.Contains(body, `value="off" selected`) {
		t.Errorf("編集フォームに現在のdefault_variation(off)が反映されていない: %s", body)
	}
	if !strings.Contains(body, `checked`) {
		t.Errorf("編集フォームに現在のenabled(true)が反映されていない: %s", body)
	}
}

func TestFeatureFlagHandlers_Edit_NotFound(t *testing.T) {
	_, router, _ := setupTestRouter(t)

	w := doRequest(router, http.MethodGet, "/flags/999999999/edit", nil, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d(存在しないID)", w.Code, http.StatusNotFound)
	}
}

func TestFeatureFlagHandlers_Update_PersistsAndRecordsAuditLog(t *testing.T) {
	gormDB, router, flag := setupTestRouter(t)
	id := strconv.FormatUint(flag.ID, 10)

	form := url.Values{
		"default_variation": {"on"},
		"enabled":           {"on"},
	}
	w := doRequest(router, http.MethodPost, "/flags/"+id, form, basicAuthHeader(testBasicAuthUser, testBasicAuthPassword))
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d(更新成功時は一覧へredirect)", w.Code, http.StatusFound)
	}
	if got := w.Header().Get("Location"); got != "/" {
		t.Errorf("Location = %q, want %q", got, "/")
	}

	// DBが実際に更新されていること
	var updated model.FeatureFlag
	if err := gormDB.First(&updated, flag.ID).Error; err != nil {
		t.Fatalf("更新後のフラグ取得に失敗: %v", err)
	}
	if diff := cmp.Diff("on", updated.DefaultVariation); diff != "" {
		t.Errorf("DefaultVariation mismatch (-want +got):\n%s", diff)
	}
	if !updated.Enabled {
		t.Errorf("Enabled = false, want true")
	}

	// feature_flag_audit_logsに1行追記されていること(changed_byはBasic Authのユーザー名)
	var logs []model.FeatureFlagAuditLog
	if err := gormDB.Where("feature_flag_id = ?", flag.ID).Find(&logs).Error; err != nil {
		t.Fatalf("監査ログ取得に失敗: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("監査ログ件数 = %d, want 1", len(logs))
	}
	if diff := cmp.Diff("off", *logs[0].BeforeDefaultVariation); diff != "" {
		t.Errorf("BeforeDefaultVariation mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("on", logs[0].AfterDefaultVariation); diff != "" {
		t.Errorf("AfterDefaultVariation mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(testBasicAuthUser, logs[0].ChangedBy); diff != "" {
		t.Errorf("ChangedBy mismatch (-want +got):\n%s", diff)
	}
}

func TestFeatureFlagHandlers_AuditLog_ShowsRecentUpdate(t *testing.T) {
	_, router, flag := setupTestRouter(t)
	id := strconv.FormatUint(flag.ID, 10)
	auth := basicAuthHeader(testBasicAuthUser, testBasicAuthPassword)

	// まず更新を1回実行し、履歴を1件作る
	form := url.Values{
		"default_variation": {"on"},
		"enabled":           {"on"},
	}
	if w := doRequest(router, http.MethodPost, "/flags/"+id, form, auth); w.Code != http.StatusFound {
		t.Fatalf("事前準備の更新に失敗: status=%d", w.Code)
	}

	w := doRequest(router, http.MethodGet, "/flags/"+id+"/audit_log", nil, auth)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, testBasicAuthUser) {
		t.Errorf("変更履歴に変更者(%s)が表示されていない", testBasicAuthUser)
	}
	if !strings.Contains(body, "off &#43; off") && !strings.Contains(body, "off → on") {
		// html/templateは矢印記号などはエスケープ不要でそのまま出るはずだが、
		// 念のため「off」と「on」がどちらも含まれていることも合わせて確認する
		if !strings.Contains(body, "off") || !strings.Contains(body, "on") {
			t.Errorf("変更履歴にbefore/afterの値(off→on)が表示されていない: %s", body)
		}
	}
}
