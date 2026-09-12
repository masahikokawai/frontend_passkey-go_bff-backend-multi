package e2e_go_rod

import (
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
)

// frontendRailsWithoutBFFBaseURL は frontend-rails/without-bff(CONTRACT.mdセクション21・22.9)の
// ベースURL。React+bff(:5173、baseURL())とは別の独立したRailsアプリのため専用の環境変数を使う
func frontendRailsWithoutBFFBaseURL() string {
	return getEnv("FRONTEND_RAILS_WITHOUT_BFF_BASE_URL", "http://localhost:5174")
}

func gotoFrontendRailsWithoutBFFPath(page *rod.Page, path string) *rod.Page {
	page.MustNavigate(frontendRailsWithoutBFFBaseURL() + path).MustWaitLoad()
	return page
}

// TestFrontendRailsWithoutBFF_PasskeyRegisterAndLogin は
// CONTRACT.mdセクション22.9(2026-09-11追記、frontend-rails/without-bffへのパスキー追加)を検証する。
// 「Keycloakでログイン→/accountでパスキー登録→ログアウト→パスキーでログイン」の一連の流れ。
//
// 【なぜこのテストが必要か】ランブックに「e2e未実装(コア構成のReact+bffフローのみが対象)」と
// 明記されていた既知のギャップだった。既存のTestPasskeyRegisterAndLogin(passkey_test.go)は
// bffのローカル認証ユーザー向けパスキー(セクション22.1)を検証するが、このアプリは全く別の対象
// (Keycloak発行ユーザー向けパスキー)を実装しているため別テストとして追加する。
//
// 【newPageWithSystemChromeを使わない理由】passkey_test.goの同名ヘルパーは、frontend(React)の
// passkey.tsが使うWebAuthn Level 3のJSON直列化API(parseCreationOptionsFromJSON等)が
// go-rod同梱のChromiumで機能検出に失敗する問題への対応だった。frontend-rails/without-bffの
// app/javascript/webauthn_codec.jsは素朴なbase64url⇔ArrayBuffer変換を自前実装しており、
// この新しいAPI群を一切呼ばない(without-bff実装時に確認済み)ため、この問題は再現しない。
// 標準のnewPage(go-rod既定のChromium)で足りる
//
// 【他のシナリオとの違い】このアプリはReactのdata-testid規約(e2e/SELECTORS.md)に従っておらず、
// 素のRails ERBビューに素朴なid属性のみを付与している。そのためこのファイルだけCSS idセレクタ・
// 表示テキストでの要素特定を使う(testid()ヘルパーは使わない)
func TestFrontendRailsWithoutBFF_PasskeyRegisterAndLogin(t *testing.T) {
	page := newPage(t)
	gotoFrontendRailsWithoutBFFPath(page, "/login")
	if err := enableVirtualAuthenticator(page); err != nil {
		t.Fatalf("enableVirtualAuthenticator() error = %v", err)
	}

	// --- Keycloakでログイン ---
	// 「Keycloakでログイン」はRailsのbutton_to(POSTフォーム送信)で、id/data-testidを持たないため、
	// 表示テキストで要素を特定する
	username := getEnv("E2E_USERNAME", "general-user")
	password := getEnv("E2E_PASSWORD", "password")
	page.MustElementR("button", "Keycloakでログイン").MustClick()
	page.MustElement("#username").MustWaitVisible().MustInput(username)
	page.MustElement("#password").MustInput(password)
	page.MustElement("#kc-login").MustClick()
	waitFrontendRailsWithoutBFFLocation(page, "/welcome", 15*time.Second)

	// --- パスキー登録: /account画面の「パスキーを登録」ボタン ---
	gotoFrontendRailsWithoutBFFPath(page, "/account")
	page.MustElement("#passkey-register-button").MustWaitVisible().MustClick()
	waitElementTextContains(t, page, "#passkey-status", "登録しました", 15*time.Second)

	// --- ログアウト ---
	// welcome.html.erbのログアウトはbutton_to(DELETE)だが、config/routes.rbには
	// 「ブラウザから直接叩いての手動確認をしやすくするため」というコメント付きでGET /logoutも
	// 明示的に許可されている。E2Eでもこの経路をそのまま使う
	gotoFrontendRailsWithoutBFFPath(page, "/logout")

	// --- ここから先はKeycloakを一切使わない、パスキーのみでのログイン ---
	gotoFrontendRailsWithoutBFFPath(page, "/login")
	page.MustElement("#passkey-login-button").MustWaitVisible().MustClick()
	waitFrontendRailsWithoutBFFLocation(page, "/welcome", 15*time.Second)

	// welcome.html.erbは`ログイン方式: <%= session[:auth_mode] %>`をそのまま出力するため、
	// auth_mode=passkeyであることをページ本文のテキストで確認する
	bodyText := page.MustElement("body").MustText()
	if !strings.Contains(bodyText, "ログイン方式: passkey") {
		t.Fatalf("welcome画面に「ログイン方式: passkey」が表示されていない。本文: %s", bodyText)
	}
}

// waitFrontendRailsWithoutBFFLocation は、現在のURLが指定パスに一致するまでポーリングで待つ
// (data-testidが無いこのアプリでは、要素待ちの代わりにURL自体を確認する)
func waitFrontendRailsWithoutBFFLocation(page *rod.Page, path string, timeout time.Duration) {
	page.Timeout(timeout).MustWait(`() => location.pathname === ` + jsStringLiteral(path))
}

// waitElementTextContains は、指定セレクタの要素のテキストが部分文字列を含むまでポーリングで待つ
func waitElementTextContains(t *testing.T, page *rod.Page, selector, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		el, err := page.Element(selector)
		if err == nil {
			text, err := el.Text()
			if err == nil && strings.Contains(text, substr) {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s が %q を %v 以内に含まなかった", selector, substr, timeout)
}

func jsStringLiteral(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
