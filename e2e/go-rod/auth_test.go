package e2e_go_rod

import (
	"testing"
	"time"
)

// TestUnauthenticated_RedirectsToLocalLogin は
// playwright版 auth.spec.ts の1つ目のシナリオ(未ログイン状態で/tasksへアクセス)に対応する
func TestUnauthenticated_RedirectsToLocalLogin(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")

	if !page.MustElement(testid("login-email-input")).MustVisible() {
		t.Fatal("login-email-input が表示されていない(ローカルログイン画面へリダイレクトされていない)")
	}
}

// TestLoginLocalHMAC_LogsInAndOut はローカル認証(HMAC版・既定)のログイン→ログアウトを検証する
func TestLoginLocalHMAC_LogsInAndOut(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	if !page.MustElement(testid("task-list")).MustVisible() {
		t.Fatal("task-list が表示されていない")
	}

	// ローカルセッションのログアウトはKeycloakのRP-Initiated Logoutを経由せず、
	// bffがFrontendBaseURLへの相対パスをそのまま返す(CONTRACT.md セクション16.4)
	page.MustElement(testid("logout-button")).MustClick()
	waitLoginScreen(page, 15*time.Second)
}

// TestLoginLocalRSA_LogsIn はローカル認証(RSA版)のログインを検証する
func TestLoginLocalRSA_LogsIn(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaLocal(page, true)

	if !page.MustElement(testid("task-list")).MustVisible() {
		t.Fatal("task-list が表示されていない(RSA版ログイン失敗)")
	}
}

// TestLoginLocal_WrongPassword_ShowsError は誤ったパスワードでのログイン失敗を検証する
func TestLoginLocal_WrongPassword_ShowsError(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/login")

	page.MustElement(testid("login-email-input")).MustWaitVisible().MustInput("local-user@example.com")
	page.MustElement(testid("login-password-input")).MustInput("wrong-password")
	page.MustElement(testid("login-submit-button")).MustClick()

	if !page.MustElement(testid("login-error")).MustWaitVisible().MustVisible() {
		t.Fatal("login-error が表示されていない")
	}
	if !page.MustElement(testid("login-email-input")).MustVisible() {
		t.Fatal("ログイン画面に留まっていない")
	}
}

// TestLoginKeycloak_LogsInAndOut はKeycloak(OIDC)でのログイン→ログアウトを検証する
func TestLoginKeycloak_LogsInAndOut(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaKeycloak(page)

	if !page.MustElement(testid("task-list")).MustVisible() {
		t.Fatal("task-list が表示されていない(Keycloakログイン失敗)")
	}

	// 【playwright版 auth.spec.ts と同じ理由】
	// ログアウト後の遷移はbffのlogout API呼び出し
	// → Keycloakのend_session_endpointへの実遷移(SSOセッション終了)
	// → post_logout_redirect_uriでフロントへ戻る→未ログイン検知で/loginへ再リダイレクト、という複数段の自動遷移になる
	// 途中のURLを待つとレースするため、収束後の最終状態だけを十分な猶予時間で待つ
	page.MustElement(testid("logout-button")).MustClick()
	waitLoginScreen(page, 15*time.Second)
}
