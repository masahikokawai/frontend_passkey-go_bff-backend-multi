// Package e2e はchromedpを使ったE2Eテスト一式。
//
// e2e/SELECTORS.mdが全フレームワーク共通の唯一の正であり、このファイルは
// e2e/playwright/tests/helpers.ts と全く同じシナリオ・同じdata-testidをchromedpで実装したもの。
package e2e

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func frontendBaseURL() string {
	return envOr("FRONTEND_BASE_URL", "http://localhost:5173")
}

// newBrowserContext は1テストにつき独立したブラウザタブ(Cookie等も独立)を用意する。
// playwright版が各testごとに新しいpageを使うのと同じ考え方(セッションを引き継がせないため)。
func newBrowserContext(t interface{ Cleanup(func()) }) context.Context {
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	t.Cleanup(func() {
		cancelCtx()
		cancelAlloc()
	})
	return ctx
}

// acceptDialogs はタスク/ラベル削除時のwindow.confirm()を即時許可する。
// chromedp特有の作法: JavaScriptDialogOpeningイベントをリッスンし、page.HandleJavaScriptDialogで応答する
// (playwright版の `page.once("dialog", (dialog) => dialog.accept())` に相当)。
func acceptDialogs(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if _, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			go func() {
				_ = chromedp.Run(ctx, page.HandleJavaScriptDialog(true))
			}()
		}
	})
}

// setReactControlledValue はReactの制御されたinput(特に<input type="date">・<select>)に
// 値を反映させるためのJS。
//
// 【このプロジェクトのSelenium e2e実装で既に踏んだ既知の問題への対応】
// 単純に `el.value = ...` するだけだと、Reactがインスタンス側でvalueのsetterを
// 上書きして「最後に自分が設定した値」を追跡しているため、後から入力イベントを
// dispatchしても「(Reactの記録上)値は変わっていない」と判定されonChangeが発火しない。
// prototype側の本来のsetterを直接呼び出すことでReactの追跡を回避し、
// その後で正しくinput/changeイベントをdispatchする(Selenium版の同種の修正と同じ考え方)。
const setReactControlledValueJS = `
function(sel, value) {
  const el = document.querySelector(sel);
  const tag = el.tagName;
  const proto = tag === 'SELECT' ? window.HTMLSelectElement.prototype : window.HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
  setter.call(el, value);
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
}
`

func setReactValue(sel, value string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf("(%s)(%q, %q)", setReactControlledValueJS, sel, value), nil)
}

// loginViaKeycloak はKeycloak標準ログインフォーム(#username/#password/#kc-login)経由でログインする。
// (playwright版 helpers.ts の loginViaKeycloak と同じシナリオ)
func loginViaKeycloak(ctx context.Context) error {
	username := envOr("E2E_USERNAME", "general-user")
	password := envOr("E2E_PASSWORD", "password")

	return chromedp.Run(ctx,
		chromedp.WaitVisible(`[data-testid="login-keycloak-button"]`, chromedp.ByQuery),
		chromedp.Click(`[data-testid="login-keycloak-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#username`, chromedp.ByQuery),
		chromedp.SendKeys(`#username`, username, chromedp.ByQuery),
		chromedp.SendKeys(`#password`, password, chromedp.ByQuery),
		chromedp.Click(`#kc-login`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-list"]`, chromedp.ByQuery),
	)
}

// loginViaLocal はローカル認証(HMAC版・既定 or RSA版)のログインフォームからログインする。
// (CONTRACT.md セクション16.1: /login はHMAC版、/login/rsa はRSA版。フォーム自体は共用)
func loginViaLocal(ctx context.Context, rsa bool) error {
	email := envOr("E2E_LOCAL_EMAIL", "local-user@example.com")
	password := envOr("E2E_LOCAL_PASSWORD", "password")

	actions := []chromedp.Action{}
	if rsa {
		// /tasksへのNavigateで既に/loginへリダイレクトされている状態から、RSA版へのリンクで遷移する
		actions = append(actions,
			chromedp.WaitVisible(`[data-testid="login-rsa-link"]`, chromedp.ByQuery),
			chromedp.Click(`[data-testid="login-rsa-link"]`, chromedp.ByQuery),
		)
	}
	actions = append(actions,
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
		chromedp.SendKeys(`[data-testid="login-email-input"]`, email, chromedp.ByQuery),
		chromedp.SendKeys(`[data-testid="login-password-input"]`, password, chromedp.ByQuery),
		chromedp.Click(`[data-testid="login-submit-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-list"]`, chromedp.ByQuery),
	)
	return chromedp.Run(ctx, actions...)
}

// pollTestIDVisible は data-testid を持つ要素が表示されるまで、Runtime.evaluate
// (chromedp.Evaluate)ベースでポーリングする。
//
// 【なぜchromedp.WaitVisibleではなくこちらを使う場面があるか、2回目のe2e監査で判明】
// chromedp.WaitVisibleはdom.QuerySelectorベースで実装されており、ナビゲーション
// (特に実ドキュメントの差し替えを伴うもの、resilience_test.goのブラウザバックのシナリオ参照)
// の直後に呼ぶと、古いドキュメントに紐づいたノード参照を掴んだままタイムアウトすることがある。
// Runtime.evaluateは常に「今の」実行コンテキストに対して評価するため、ドキュメントの
// 差し替えをまたいでも安定して動く。
func pollTestIDVisible(ctx context.Context, testid string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	js := fmt.Sprintf(`(function(){var el=document.querySelector('[data-testid="%s"]');return !!el && el.offsetParent !== null;})()`, testid)
	for time.Now().Before(deadline) {
		var visible bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &visible)); err == nil && visible {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("data-testid=%q が%v以内に表示されなかった", testid, timeout)
}

// uniqueTaskName はTask.nameの20文字以内バリデーション(Rails/Go両方共通)に収まるよう
// 短いユニーク名を生成する(playwright版 uniqueTaskName と同じ)。
func uniqueTaskName(prefix string) string {
	if prefix == "" {
		prefix = "E2E"
	}
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()/1_000_000%100000)
}
