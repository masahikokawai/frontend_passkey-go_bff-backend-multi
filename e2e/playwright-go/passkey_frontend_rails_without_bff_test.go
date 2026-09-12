package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// frontendRailsWithoutBFFBaseURL は frontend-rails/without-bff(CONTRACT.mdセクション21・22.9)の
// ベースURL。React+bff(baseURL)とは別の独立したRailsアプリのため専用の環境変数を使う
// (e2e/chromedp・e2e/go-rod・e2e/playwrightの同名関数と同じ考え方)
func frontendRailsWithoutBFFBaseURL() string {
	return envOr("FRONTEND_RAILS_WITHOUT_BFF_BASE_URL", "http://localhost:5174")
}

// waitElementTextContains は、指定ロケータのテキストが部分文字列を含むまでポーリングで待つ
// (e2e/chromedp・e2e/go-rodの同名ヘルパーと同じ考え方。このアプリはdata-testidを持たないため
// 要素待ちの代わりにテキスト内容自体をポーリングする)
func waitElementTextContains(t *testing.T, locator playwright.Locator, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		text, err := locator.TextContent()
		if err == nil && strings.Contains(text, substr) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("要素のテキストが %q を %v 以内に含まなかった", substr, timeout)
}

// TestFrontendRailsWithoutBFF_PasskeyRegisterAndLogin は
// CONTRACT.mdセクション22.9(2026-09-11追記、frontend-rails/without-bffへのパスキー追加)を検証する。
// 「Keycloakでログイン→/accountでパスキー登録→ログアウト→パスキーでログイン」の一連の流れ。
//
// 【なぜこのテストが必要か】このシナリオはランブックに「e2e未実装(コア構成のReact+bffフローのみが
// 対象)」と明記されていた既知のギャップだった。既存のTestPasskeyRegisterAndLogin(passkey_test.go)は
// bffのローカル認証ユーザー向けパスキー(セクション22.1)を検証するが、このアプリは全く別の対象
// (Keycloak発行ユーザー向けパスキー)を実装しているため、別テストとして追加する。
//
// 【他のシナリオとの違い】このアプリはReactのdata-testid規約(e2e/SELECTORS.md)に従っておらず、
// 素のRails ERBビューに素朴なid属性のみを付与している(login.html.erb/account.html.erb参照)。
// そのためこのファイルだけ`page.Locator`によるCSS idセレクタ・テキストセレクタを使う
// (`page.GetByTestId`は使わない)。仮想認証器の登録自体はTestPasskeyRegisterAndLoginと
// 完全に同じCDPパラメータを使う(chromedp/go-rod/playwright版とも揃えてある)
func TestFrontendRailsWithoutBFF_PasskeyRegisterAndLogin(t *testing.T) {
	page := newPage(t)

	session, err := page.Context().NewCDPSession(page)
	if err != nil {
		t.Fatalf("CDPSessionの作成に失敗しました: %v", err)
	}
	if _, err := session.Send("WebAuthn.enable", map[string]any{}); err != nil {
		t.Fatalf("WebAuthn.enableに失敗しました: %v", err)
	}
	_, err = session.Send("WebAuthn.addVirtualAuthenticator", map[string]any{
		"options": map[string]any{
			"protocol":                    "ctap2",
			"transport":                   "internal",
			"hasResidentKey":              true,
			"hasUserVerification":         true,
			"isUserVerified":              true,
			"automaticPresenceSimulation": true,
		},
	})
	if err != nil {
		t.Fatalf("WebAuthn.addVirtualAuthenticatorに失敗しました: %v", err)
	}

	if _, err := page.Goto(frontendRailsWithoutBFFBaseURL() + "/login"); err != nil {
		t.Fatalf("Goto(/login)に失敗しました: %v", err)
	}

	// --- Keycloakでログイン ---
	// 「Keycloakでログイン」はRailsのbutton_to(POSTフォーム送信)で、id/data-testidを持たないため、
	// テキストセレクタで要素を特定する(chromedp版のXPathテキスト一致・go-rod版のMustElementRと同じ考え方)
	username := envOr("E2E_USERNAME", "general-user")
	password := envOr("E2E_PASSWORD", "password")
	if err := page.Locator("text=Keycloakでログイン").Click(); err != nil {
		t.Fatalf("「Keycloakでログイン」のクリックに失敗しました: %v", err)
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
	waitURLOpts := playwright.PageWaitForURLOptions{Timeout: playwright.Float(15000)}
	if err := page.WaitForURL(frontendRailsWithoutBFFBaseURL()+"/welcome", waitURLOpts); err != nil {
		t.Fatalf("Keycloakログイン後の/welcome遷移待ちに失敗: %v", err)
	}

	// --- パスキー登録: /account画面の「パスキーを登録」ボタン ---
	if _, err := page.Goto(frontendRailsWithoutBFFBaseURL() + "/account"); err != nil {
		t.Fatalf("Goto(/account)に失敗しました: %v", err)
	}
	registerButton := page.Locator("#passkey-register-button")
	mustWaitVisible(t, registerButton, "#passkey-register-button")
	if err := registerButton.Click(); err != nil {
		t.Fatalf("passkey-register-buttonのクリックに失敗しました: %v", err)
	}
	waitElementTextContains(t, page.Locator("#passkey-status"), "登録しました", 15*time.Second)

	// --- ログアウト ---
	// welcome.html.erbのログアウトはbutton_to(DELETE)だが、config/routes.rbには
	// 「ブラウザから直接叩いての手動確認をしやすくするため」というコメント付きでGET /logoutも
	// 明示的に許可されている。E2Eでもこの経路をそのまま使う
	if _, err := page.Goto(frontendRailsWithoutBFFBaseURL() + "/logout"); err != nil {
		t.Fatalf("ログアウトのGotoに失敗しました: %v", err)
	}

	// --- ここから先はKeycloakを一切使わない、パスキーのみでのログイン ---
	if _, err := page.Goto(frontendRailsWithoutBFFBaseURL() + "/login"); err != nil {
		t.Fatalf("Goto(/login)に失敗しました(2回目): %v", err)
	}
	passkeyLoginButton := page.Locator("#passkey-login-button")
	mustWaitVisible(t, passkeyLoginButton, "#passkey-login-button")
	if err := passkeyLoginButton.Click(); err != nil {
		t.Fatalf("パスキーでのログインボタンのクリックに失敗しました: %v", err)
	}
	if err := page.WaitForURL(frontendRailsWithoutBFFBaseURL()+"/welcome", waitURLOpts); err != nil {
		t.Fatalf("パスキーログイン後の/welcome遷移待ちに失敗: %v", err)
	}

	// welcome.html.erbは`ログイン方式: <%= session[:auth_mode] %>`をそのまま出力するため、
	// auth_mode=passkeyであることをページ本文のテキストで確認する
	bodyText, err := page.Locator("body").TextContent()
	if err != nil {
		t.Fatalf("ページ本文の取得に失敗しました: %v", err)
	}
	if !strings.Contains(bodyText, "ログイン方式: passkey") {
		t.Fatalf("welcome画面に「ログイン方式: passkey」が表示されていない。本文: %s", bodyText)
	}
}
