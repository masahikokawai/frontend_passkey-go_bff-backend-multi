package e2e

import "testing"

// TestUnauthenticatedRedirectsToLocalLogin はJS版auth.spec.tsの
// 「未ログイン状態で/tasksへアクセスするとローカルログイン画面(HMAC版・既定)へ
// リダイレクトされる」に対応する
func TestUnauthenticatedRedirectsToLocalLogin(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	mustWaitVisible(t, page.GetByTestId("login-email-input"), "login-email-input")
}

// TestLoginLocalHMAC_TaskListAndLogout はJS版の
// 「ローカル認証(HMAC版・既定)でログイン→タスク一覧表示→ログアウト→ログイン画面に戻る」
func TestLoginLocalHMAC_TaskListAndLogout(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)
	mustWaitVisible(t, page.GetByTestId("task-list"), "task-list")

	if err := page.GetByTestId("logout-button").Click(); err != nil {
		t.Fatalf("ログアウトボタンのクリックに失敗しました: %v", err)
	}
	waitForLoginScreen(t, page)
}

// TestLoginLocalRSA_TaskList はJS版の「ローカル認証(RSA版)でログイン→タスク一覧表示」
func TestLoginLocalRSA_TaskList(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaLocal(t, page, true)
	mustWaitVisible(t, page.GetByTestId("task-list"), "task-list")
}

// TestLoginLocalWrongPassword_ShowsError はJS版の
// 「誤ったパスワードでのログインはエラーメッセージを表示し、ログイン画面に留まる」
func TestLoginLocalWrongPassword_ShowsError(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/login"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	if err := page.GetByTestId("login-email-input").Fill("local-user@example.com"); err != nil {
		t.Fatalf("メールアドレス入力に失敗しました: %v", err)
	}
	if err := page.GetByTestId("login-password-input").Fill("wrong-password"); err != nil {
		t.Fatalf("パスワード入力に失敗しました: %v", err)
	}
	if err := page.GetByTestId("login-submit-button").Click(); err != nil {
		t.Fatalf("ログインボタンのクリックに失敗しました: %v", err)
	}

	mustWaitVisible(t, page.GetByTestId("login-error"), "login-error")

	visible, err := page.GetByTestId("login-email-input").IsVisible()
	if err != nil {
		t.Fatalf("login-email-inputの表示確認に失敗しました: %v", err)
	}
	if !visible {
		t.Errorf("誤ったパスワード後もログイン画面(login-email-input)に留まっているはずが、留まっていない")
	}
}

// TestLoginKeycloak_TaskListAndLogout はJS版の
// 「Keycloakでログイン→タスク一覧表示→ログアウト→ログイン画面に戻る」
func TestLoginKeycloak_TaskListAndLogout(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaKeycloak(t, page)
	mustWaitVisible(t, page.GetByTestId("task-list"), "task-list")

	if err := page.GetByTestId("logout-button").Click(); err != nil {
		t.Fatalf("ログアウトボタンのクリックに失敗しました: %v", err)
	}
	waitForLoginScreen(t, page)
}
