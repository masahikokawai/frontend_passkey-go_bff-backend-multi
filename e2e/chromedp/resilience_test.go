// resilience_test.go は e2e/playwright/tests/resilience.spec.ts の4シナリオを
// chromedpで実装したもの(2回目のe2e監査で追加)。「壊れやすいのに見落とされがちな、
// アプリの土台部分の挙動」(ネットワーク遅延・ブラウザ操作・複数タブ・認証手段の後方互換)を対象にする。
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

// delayTasksAPI は /api/tasks* へのリクエストにだけ人為的な遅延を上乗せする。
//
// 【なぜCDPのFetchドメインか】chromedpにはPlaywrightのpage.route()に相当する
// 高レベルAPIが無いため、CDPのFetchドメイン(リクエストを一時停止させ、
// 明示的にContinueRequestするまで待たせる)を直接使う。acceptDialogs(helpers.go)と
// 同じ「ListenTargetでイベントを受け、非同期にchromedp.Runで応答する」パターンを踏襲する
// (webauthn.go同様、Do(ctx)を素のctxへ直接呼ぶとinvalid contextになるため)。
func delayTasksAPI(ctx context.Context, delay time.Duration) error {
	if err := chromedp.Run(ctx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*/api/tasks*"}}),
	); err != nil {
		return err
	}
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		req, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			time.Sleep(delay)
			_ = chromedp.Run(ctx, fetch.ContinueRequest(req.RequestID))
		}()
	})
	return nil
}

func TestResilience_SlowNetwork_ShowsLoadingState(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	if err := delayTasksAPI(ctx, 1500*time.Millisecond); err != nil {
		t.Fatalf("delayTasksAPI() error = %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	// ログイン成功→リダイレクト後、再度/api/tasksを呼ぶ実装のため、遅延を維持したまま
	// リロードし、その瞬間に「読み込み中...」が見えることを確認する(Playwright版と同じ考え方)
	if err := chromedp.Run(ctx, chromedp.Reload()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	// テキスト内容での検索はchromedp.BySearch(DOM.performSearchベース)を使う。
	// XPathのcontains(text(),...)でDOM上のテキストノードを直接探す
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`//*[contains(text(), "読み込み中")]`, chromedp.BySearch)); err != nil {
		t.Fatalf("「読み込み中...」が表示されなかった: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`[data-testid="task-list"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("task-listが表示されなかった: %v", err)
	}
}

func TestResilience_BrowserBack_PreservesAuthAndState(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/labels")); err != nil {
		t.Fatalf("labels画面へのNavigate() error = %v", err)
	}
	var url string
	if err := chromedp.Run(ctx, chromedp.Location(&url)); err != nil {
		t.Fatalf("Location() error = %v", err)
	}
	if url == "" {
		t.Fatal("URLの取得に失敗した")
	}

	// 【実機検証で判明】chromedp.NavigateBack()(CDPのPage.navigateToHistoryEntry)は、
	// 内部的にPage.loadEventFiredを待つ実装になっているが、React RouterのようなSPAの
	// クライアントサイド遷移(pushState)で作られた履歴エントリへ戻る場合はドキュメントの
	// 再読み込みが発生せずloadイベントも発火しないため、いつまでもタイムアウトするまで
	// ブロックしてしまう。ブラウザの戻るボタンと同じ効果を持つJS(history.back())を
	// 直接呼ぶことで、CDPのナビゲーション完了待ちを回避する
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.history.back()`, nil)); err != nil {
		t.Fatalf("history.back() error = %v", err)
	}
	// 【実機検証で判明・追記】/labelsへは実ナビゲーション(chromedp.Navigate)で遷移しているため、
	// history.back()で/tasksへ戻る際もブラウザは実際にドキュメントを再読み込みする
	// (SPAのpushStateだけで済むケースとは異なり、直前に本物のNavigateを挟むと、
	// それより前のpushState履歴エントリへ戻る操作はブラウザにとって「別ドキュメントへの
	// 実ナビゲーション」相当になる)。この再読み込みの最中にdom.QuerySelectorベースの
	// chromedp.WaitVisibleを呼ぶと、ナビゲーション前のドキュメントに紐づいた古いDOMノード
	// 参照を使い続けてしまい、いつまでも要素が見つからずタイムアウトする(実機で再現・確認済み)。
	// Runtime.evaluateベースのpollTestID(helpers.go)は都度「今の」実行コンテキストに
	// 対して評価するため、この種のドキュメント差し替えをまたいでも正しく動作する。
	if err := pollTestIDVisible(ctx, "task-list", 15*time.Second); err != nil {
		t.Fatalf("戻った後にtask-listが表示されなかった(再ログインを要求された可能性): %v", err)
	}
}

func TestResilience_Reload_KeepsSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Reload(),
		chromedp.WaitVisible(`[data-testid="task-list"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("リロード後にtask-listが表示されなかった: %v", err)
	}

	var loginInputCount int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('[data-testid="login-email-input"]').length`, &loginInputCount,
	)); err != nil {
		t.Fatalf("login-email-inputの有無確認に失敗: %v", err)
	}
	if loginInputCount != 0 {
		t.Errorf("リロード後にログイン画面へ飛ばされている(login-email-inputが%d件存在)", loginInputCount)
	}
}

// TestResilience_MultiTab_SessionSharedAndLogoutPropagates は
// 「片方のタブでログイン→もう片方のタブでも認証済み」「片方でログアウト→もう片方も次のアクセスで未ログイン扱い」を確認する。
//
// 【なぜchromedp.NewContext(ctx)で第2タブを開くか】chromedpでは、既にブラウザを持つcontext
// (chromedp.NewContextで作られたctx)を親にしてもう一度chromedp.NewContextを呼ぶと、
// 新しいブラウザプロセスを起動するのではなく「同じブラウザ内に新しいタブ(target)」を開く。
// 同じブラウザプロセス=同じCookie jarのため、Playwright版のcontext.newPage()
// (同一BrowserContext内の新規ページ)と同じ状況を再現できる。
func TestResilience_MultiTab_SessionSharedAndLogoutPropagates(t *testing.T) {
	tab1Ctx, cancel := context.WithTimeout(newBrowserContext(t), 40*time.Second)
	defer cancel()

	if err := chromedp.Run(tab1Ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("tab1 Navigate() error = %v", err)
	}
	if err := loginViaLocal(tab1Ctx, false); err != nil {
		t.Fatalf("tab1 loginViaLocal() error = %v", err)
	}

	tab2Ctx, cancelTab2 := chromedp.NewContext(tab1Ctx)
	defer cancelTab2()
	if err := chromedp.Run(tab2Ctx,
		chromedp.Navigate(frontendBaseURL()+"/tasks"),
		chromedp.WaitVisible(`[data-testid="task-list"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("tab2(同じブラウザの新タブ)がログイン済み扱いにならなかった: %v", err)
	}

	// tab1でログアウト(bff側のRedisセッションが削除される)
	if err := chromedp.Run(tab1Ctx,
		chromedp.Click(`[data-testid="logout-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("tab1でのログアウトに失敗: %v", err)
	}

	// tab2はリロードして初めてサーバー側のセッション失効に気づく
	loginCtx, cancelLogin := context.WithTimeout(tab2Ctx, 15*time.Second)
	defer cancelLogin()
	if err := chromedp.Run(loginCtx,
		chromedp.Reload(),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("tab1でのログアウト後、tab2がログイン画面に戻らなかった: %v", err)
	}
}

// TestResilience_PasskeyRegistration_DoesNotBreakPasswordLogin は
// パスキー機能追加(auth_mode="passkey"分岐等の共通コード変更)が、既存のパスワードログイン経路を
// 壊していないことを、「パスキー登録済み」の状態から改めて確認する回帰テスト。
func TestResilience_PasskeyRegistration_DoesNotBreakPasswordLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := enableVirtualAuthenticator(ctx); err != nil {
		t.Fatalf("enableVirtualAuthenticator() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(frontendBaseURL()+"/account"),
		chromedp.WaitVisible(`[data-testid="passkey-register-button"]`, chromedp.ByQuery),
		chromedp.Click(`[data-testid="passkey-register-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="passkey-register-success"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("パスキー登録に失敗: %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-testid="logout-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("ログアウトに失敗: %v", err)
	}

	// ここが本題: パスキーボタンではなく、通常通りメールアドレス+パスワードでログインし直す
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("パスキー登録後のパスワードログインに失敗: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`[data-testid="task-list"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("パスワードログイン後にtask-listが表示されなかった: %v", err)
	}
}

