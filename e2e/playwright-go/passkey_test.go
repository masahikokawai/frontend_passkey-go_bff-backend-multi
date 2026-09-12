package e2e

import "testing"

// TestPasskeyRegisterAndLogin は
// 「ローカル認証(HMAC)でログイン→/accountでパスキー登録→ログアウト→パスキーでログイン」を検証する
// CONTRACT.mdセクション22.1により対象はbffのローカル認証ユーザーのみ(Keycloak発行ユーザーは対象外)
//
// 【なぜこの実装にしたか】
// playwright-go(このバインディング)にはWebAuthn専用の高レベルAPIが無い(JS版Playwrightも同様)
// しかしChromium限定で`BrowserContext.NewCDPSession(page)`から
// 生のCDPコマンドを送れるため、e2e/chromedp・e2e/go-rodと全く同じ設定
// (protocol/transport/hasResidentKey等)を生のJSONパラメータとして送るだけで、同じ仮想認証器を登録できる
// 3つのCDP系フレームワーク(chromedp/go-rod/playwright-go)で
// 設定値を完全に揃えることで、「同じCDP機能に、各ライブラリがどれだけ高レベルAPIを
// 被せているか」の対比にもなる(chromedp/go-rodは型付きのプロトコル定義を直接呼べるが、
// playwright-goは map[string]any の生パラメータを手で組み立てる必要がある)
func TestPasskeyRegisterAndLogin(t *testing.T) {
	page := newPage(t)

	session, err := page.Context().NewCDPSession(page)
	if err != nil {
		t.Fatalf("CDPSessionの作成に失敗しました: %v", err)
	}
	if _, err := session.Send("WebAuthn.enable", map[string]any{}); err != nil {
		t.Fatalf("WebAuthn.enableに失敗しました: %v", err)
	}
	// e2e/chromedp/passkey_test.go・e2e/go-rod/passkey_test.goと完全に同じ設定
	// (hasResidentKey: discoverable credentialの検証に必須、automaticPresenceSimulation:
	// CIで人手のタッチ操作を待たせない)
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

	if _, err := page.Goto(baseURL + "/tasks"); err != nil {
		t.Fatalf("Goto(/tasks)に失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	// パスキー登録: /account画面の「パスキーを登録」ボタン
	if _, err := page.Goto(baseURL + "/account"); err != nil {
		t.Fatalf("Goto(/account)に失敗しました: %v", err)
	}
	registerButton := page.GetByTestId("passkey-register-button")
	mustWaitVisible(t, registerButton, "passkey-register-button")
	if err := registerButton.Click(); err != nil {
		t.Fatalf("passkey-register-buttonのクリックに失敗しました: %v", err)
	}
	successAlert := page.GetByTestId("passkey-register-success")
	mustWaitVisible(t, successAlert, "passkey-register-success")

	// 一度ログアウトし、パスキーのみ(Keycloak・パスワード不要)でログインできることを検証する
	if err := page.GetByTestId("logout-button").Click(); err != nil {
		t.Fatalf("logout-buttonのクリックに失敗しました: %v", err)
	}
	loginEmailInput := page.GetByTestId("login-email-input")
	mustWaitVisible(t, loginEmailInput, "login-email-input")

	passkeyLoginButton := page.GetByTestId("login-passkey-button")
	mustWaitVisible(t, passkeyLoginButton, "login-passkey-button")
	if err := passkeyLoginButton.Click(); err != nil {
		t.Fatalf("login-passkey-buttonのクリックに失敗しました: %v", err)
	}
	if err := page.GetByTestId("task-list").WaitFor(); err != nil {
		t.Fatalf("パスキーでのログイン後にtask-listが表示されない: %v", err)
	}
}
