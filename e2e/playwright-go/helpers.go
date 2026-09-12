package e2e

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// envOr は環境変数を読み、未設定なら既定値を返す
//
// 【実機検証で発覚した不具合】
//
// 以前はmain_test.go(_testファイル)にだけ定義されており、
// go testでは問題なく動くが、go build ./...・go vet ./...(_testファイルを対象外にする)は「undefined: envOr」で失敗していた
// helpers.go(非testファイル)側から呼ばれる関数は非testファイルに置く必要がある
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loginViaLocal はローカル認証(HMAC版・既定 or RSA版)のログインフォームからログインする
// (CONTRACT.mdセクション16.1: /login はHMAC版、/login/rsa はRSA版
// フォーム自体は同じコンポーネント(LoginForm.tsx)を共用しているJS版helpers.tsのGo版)
func loginViaLocal(t *testing.T, page playwright.Page, rsa bool) {
	t.Helper()
	email := envOr("E2E_LOCAL_EMAIL", "local-user@example.com")
	password := envOr("E2E_LOCAL_PASSWORD", "password")

	if rsa {
		// /tasksへのgotoで既に/loginへリダイレクトされている状態から、RSA版へのリンクで遷移する
		rsaLink := page.GetByTestId("login-rsa-link")
		mustWaitVisible(t, rsaLink, "login-rsa-link")
		if err := rsaLink.Click(); err != nil {
			t.Fatalf("login-rsa-linkのクリックに失敗しました: %v", err)
		}
	}

	emailInput := page.GetByTestId("login-email-input")
	mustWaitVisible(t, emailInput, "login-email-input")
	if err := emailInput.Fill(email); err != nil {
		t.Fatalf("メールアドレス入力に失敗しました: %v", err)
	}
	if err := page.GetByTestId("login-password-input").Fill(password); err != nil {
		t.Fatalf("パスワード入力に失敗しました: %v", err)
	}
	if err := page.GetByTestId("login-submit-button").Click(); err != nil {
		t.Fatalf("ログインボタンのクリックに失敗しました: %v", err)
	}

	mustWaitVisible(t, page.GetByTestId("task-list"), "task-list")
}

// loginViaKeycloak はKeycloak標準ログインフォーム経由でログインする
func loginViaKeycloak(t *testing.T, page playwright.Page) {
	t.Helper()
	username := envOr("E2E_USERNAME", "general-user")
	password := envOr("E2E_PASSWORD", "password")

	kcButton := page.GetByTestId("login-keycloak-button")
	mustWaitVisible(t, kcButton, "login-keycloak-button")
	if err := kcButton.Click(); err != nil {
		t.Fatalf("login-keycloak-buttonのクリックに失敗しました: %v", err)
	}

	usernameInput := page.Locator("#username")
	mustWaitVisible(t, usernameInput, "#username(Keycloakログインフォーム)")
	if err := usernameInput.Fill(username); err != nil {
		t.Fatalf("Keycloakユーザー名入力に失敗しました: %v", err)
	}
	if err := page.Locator("#password").Fill(password); err != nil {
		t.Fatalf("Keycloakパスワード入力に失敗しました: %v", err)
	}
	if err := page.Locator("#kc-login").Click(); err != nil {
		t.Fatalf("Keycloakログインボタンのクリックに失敗しました: %v", err)
	}

	mustWaitVisible(t, page.GetByTestId("task-list"), "task-list")
}

func uniqueTaskName(prefix string) string {
	if prefix == "" {
		prefix = "E2E"
	}
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%100000)
}

// waitForLoginScreen はログイン画面(login-email-input)が表示されるまで、
// 十分な猶予時間(15秒)で待つ
//
// Keycloakのログアウトは複数回のリダイレクトを経る(bffの/api/auth/logout
// → window.location.hrefでKeycloakのend_session_endpointへ実遷移
// → post_logout_redirect_uriでfrontendへ戻る → ProtectedLayoutが未ログインを検知し/loginへ再リダイレクト)
// 途中の特定URLを待つと、その後さらに自動
// リダイレクトが続いている最中に次の操作が割り込んでしまうことがあるため、
// 一連の自動リダイレクトが収束した最終状態だけを待つ(JS版auth.spec.tsと同じ方針)
func waitForLoginScreen(t *testing.T, page playwright.Page) {
	t.Helper()
	err := page.GetByTestId("login-email-input").WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(15000),
	})
	if err != nil {
		t.Fatalf("ログアウト後にログイン画面(login-email-input)が表示されない: %v", err)
	}
}

func mustWaitVisible(t *testing.T, locator playwright.Locator, name string) {
	t.Helper()
	if err := locator.WaitFor(); err != nil {
		t.Fatalf("%sが表示されない: %v", name, err)
	}
}
