package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// frontendRailsWithoutBFFBaseURL は frontend-rails/without-bff(CONTRACT.mdセクション21・22.9)の
// ベースURL。React+bff(:5173、frontendBaseURL())とは別の独立したRailsアプリのため、
// 専用の環境変数で切り替えられるようにする
func frontendRailsWithoutBFFBaseURL() string {
	return envOr("FRONTEND_RAILS_WITHOUT_BFF_BASE_URL", "http://localhost:5174")
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
// 素のRails ERBビューに素朴なid属性のみを付与している(login.html.erb/account.html.erbの実装参照)。
// そのためこのファイルだけCSS idセレクタ・XPathテキスト一致を使う(他ファイルの`testid`ヘルパー相当は無い)。
func TestFrontendRailsWithoutBFF_PasskeyRegisterAndLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 40*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendRailsWithoutBFFBaseURL()+"/login")); err != nil {
		t.Fatalf("Navigate(/login) error = %v", err)
	}
	// 仮想認証器の有効化はnavigator.credentials.*呼び出しより前であればよい(chromedp版passkey_test.goと同じ方針)
	if err := enableVirtualAuthenticator(ctx); err != nil {
		t.Fatalf("enableVirtualAuthenticator() error = %v", err)
	}

	// --- Keycloakでログイン ---
	// 「Keycloakでログイン」はRailsのbutton_to(POSTフォーム送信)で、id/data-testidを持たないため、
	// 表示テキストで要素を特定する(chromedp.BySearchはCSS/XPathどちらのクエリも自動判別する)
	username := envOr("E2E_USERNAME", "general-user")
	password := envOr("E2E_PASSWORD", "password")
	err := chromedp.Run(ctx,
		chromedp.Click(`//button[contains(., "Keycloakでログイン")]`, chromedp.BySearch),
		chromedp.WaitVisible(`#username`, chromedp.ByQuery),
		chromedp.SendKeys(`#username`, username, chromedp.ByQuery),
		chromedp.SendKeys(`#password`, password, chromedp.ByQuery),
		chromedp.Click(`#kc-login`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("Keycloakログインに失敗: %v", err)
	}
	if err := waitFrontendRailsWithoutBFFLocation(ctx, "/welcome", 15*time.Second); err != nil {
		t.Fatalf("Keycloakログイン後の/welcome遷移待ちに失敗: %v", err)
	}

	// --- パスキー登録: /account画面の「パスキーを登録」ボタン ---
	err = chromedp.Run(ctx,
		chromedp.Navigate(frontendRailsWithoutBFFBaseURL()+"/account"),
		chromedp.WaitVisible(`#passkey-register-button`, chromedp.ByQuery),
		chromedp.Click(`#passkey-register-button`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("パスキー登録ボタンのクリックに失敗: %v", err)
	}
	if err := waitElementTextContains(ctx, `#passkey-status`, "登録しました", 15*time.Second); err != nil {
		t.Fatalf("パスキー登録に失敗: %v", err)
	}

	// --- ログアウト ---
	// 【実装判断】welcome.html.erbのログアウトはbutton_to(DELETE)だが、config/routes.rbには
	// 「ブラウザから直接叩いての手動確認をしやすくするため」というコメント付きでGET /logoutも
	// 明示的に許可されている。E2Eでもこの経路をそのまま使う(ボタンのテキスト一致より単純で確実)
	if err := chromedp.Run(ctx, chromedp.Navigate(frontendRailsWithoutBFFBaseURL()+"/logout")); err != nil {
		t.Fatalf("ログアウトのNavigateに失敗: %v", err)
	}

	// --- ここから先はKeycloakを一切使わない、パスキーのみでのログイン ---
	loginCtx, cancelLogin := context.WithTimeout(ctx, 15*time.Second)
	defer cancelLogin()
	err = chromedp.Run(loginCtx,
		chromedp.Navigate(frontendRailsWithoutBFFBaseURL()+"/login"),
		chromedp.WaitVisible(`#passkey-login-button`, chromedp.ByQuery),
		chromedp.Click(`#passkey-login-button`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("パスキーでのログインに失敗: %v", err)
	}
	if err := waitFrontendRailsWithoutBFFLocation(ctx, "/welcome", 15*time.Second); err != nil {
		t.Fatalf("パスキーログイン後の/welcome遷移待ちに失敗: %v", err)
	}

	// welcome.html.erbは`ログイン方式: <%= session[:auth_mode] %>`をそのまま出力するため、
	// auth_mode=passkeyであることをページ本文のテキストで確認する
	var bodyText string
	if err := chromedp.Run(ctx, chromedp.Text("body", &bodyText, chromedp.ByQuery)); err != nil {
		t.Fatalf("ページ本文の取得に失敗: %v", err)
	}
	if !strings.Contains(bodyText, "ログイン方式: passkey") {
		t.Fatalf("welcome画面に「ログイン方式: passkey」が表示されていない。本文: %s", bodyText)
	}
}

// waitFrontendRailsWithoutBFFLocation は、frontend-rails/without-bffの現在のURLが
// 指定パスに一致するまで待つ(location.pathnameをポーリング)
//
// 【なぜ必要か】pollTestIDVisibleと同じ理由(ドキュメント差し替えをまたぐナビゲーション待ち)に加え、
// このアプリはdata-testidを持たないため、要素待ちの代わりにURL自体を確認する
func waitFrontendRailsWithoutBFFLocation(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var pathname string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname`, &pathname)); err == nil && pathname == path {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("location.pathname が %q に %v 以内にならなかった", path, timeout)
}

// waitElementTextContains は、指定セレクタの要素のテキストが部分文字列を含むまで待つ
// (pollTestIDVisibleと同じポーリング方式。data-testidが無いこのアプリ専用のヘルパー)
func waitElementTextContains(ctx context.Context, selector, substr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	js := `(function(){var el=document.querySelector('` + selector + `');return el ? el.textContent : "";})()`
	for time.Now().Before(deadline) {
		var text string
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &text)); err == nil && strings.Contains(text, substr) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s が %q を %v 以内に含まなかった", selector, substr, timeout)
}
