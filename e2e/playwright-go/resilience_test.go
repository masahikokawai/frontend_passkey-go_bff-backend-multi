package e2e

// resilience_test.go は e2e/playwright/tests/resilience.spec.ts の4シナリオを
// playwright-goで実装したもの(2回目のe2e監査で追加)
//
// 「壊れやすいのに見落とされがちな、
// アプリの土台部分の挙動」(ネットワーク遅延・ブラウザ操作・複数タブ・認証手段の後方互換)を対象にする
//
// 【なぜJS版とほぼ1対1で移植できたか】playwright-goはPlaywright本体(Node製ドライバ)を
// Goから操作するバインディングであり、chromedp/go-rodのような「生のCDPを自分で組み立てる」レイヤーが無い
// route()・goBack()・context.newPage()等、JS版が使っている高レベルAPIが
// そのままGo版にも存在するため、シナリオの構造をほぼそのまま移植できる
// (go-rodのresilience_test.goで発生した複数タブまわりの不安定さは、playwright-goでは
// 実機検証の範囲では再現しなかった
// 実際のPlaywrightドライバが多重化を面倒見ているためと思われる)
import (
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

func TestResilience_SlowNetwork_ShowsLoadingState(t *testing.T) {
	page := newPage(t)

	err := page.Route("**/api/tasks*", func(route playwright.Route) {
		time.Sleep(1500 * time.Millisecond)
		_ = route.Continue()
	})
	if err != nil {
		t.Fatalf("Route()の設定に失敗しました: %v", err)
	}

	if _, err := page.Goto(baseURL + "/tasks"); err != nil {
		t.Fatalf("Goto(/tasks)に失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	// ログイン成功→リダイレクト後、再度/api/tasksを呼ぶ実装のため、遅延を維持したままリロードし、
	// その瞬間に「読み込み中...」が見えることを確認する(JS版と同じ考え方)
	if _, err := page.Reload(); err != nil {
		t.Fatalf("Reload()に失敗しました: %v", err)
	}
	loadingText := page.GetByText("読み込み中...")
	if err := loadingText.WaitFor(); err != nil {
		t.Fatalf("「読み込み中...」が表示されなかった: %v", err)
	}
	if err := page.GetByTestId("task-list").WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(10000),
	}); err != nil {
		t.Fatalf("task-listが表示されなかった: %v", err)
	}
}

func TestResilience_BrowserBack_PreservesAuthAndState(t *testing.T) {
	page := newPage(t)

	if _, err := page.Goto(baseURL + "/tasks"); err != nil {
		t.Fatalf("Goto(/tasks)に失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	if _, err := page.Goto(baseURL + "/labels"); err != nil {
		t.Fatalf("Goto(/labels)に失敗しました: %v", err)
	}

	if _, err := page.GoBack(); err != nil {
		t.Fatalf("GoBack()に失敗しました: %v", err)
	}
	// 戻った後も再ログインを要求されず、一覧がそのまま表示されることを確認する
	if err := page.GetByTestId("task-list").WaitFor(); err != nil {
		t.Fatalf("戻った後にtask-listが表示されなかった(再ログインを要求された可能性): %v", err)
	}
}

func TestResilience_Reload_KeepsSession(t *testing.T) {
	page := newPage(t)

	if _, err := page.Goto(baseURL + "/tasks"); err != nil {
		t.Fatalf("Goto(/tasks)に失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	if _, err := page.Reload(); err != nil {
		t.Fatalf("Reload()に失敗しました: %v", err)
	}
	if err := page.GetByTestId("task-list").WaitFor(); err != nil {
		t.Fatalf("リロード後にtask-listが表示されなかった: %v", err)
	}
	count, err := page.GetByTestId("login-email-input").Count()
	if err != nil {
		t.Fatalf("login-email-inputの件数取得に失敗しました: %v", err)
	}
	if count != 0 {
		t.Errorf("リロード後にログイン画面へ飛ばされている(login-email-inputが%d件存在)", count)
	}
}

// TestResilience_MultiTab_SessionSharedAndLogoutPropagates は
// 「片方のタブでログインすると、同じCookieを共有するもう片方のタブでも認証済み扱いになる」
// 「片方のタブでログアウトすると、もう片方のタブは次のアクセスでログイン画面に戻る」を確認する
//
// 【go-rod版との対比】go-rod版resilience_test.goでは、同じブラウザ内の2つ目のタブへの
// 操作がCDPレベルでハングする問題に何度も当たり、後半のログアウト伝播シナリオを断念した
//
// playwright-go では同じ流れ(page.Context().NewPage()で同一 BrowserContext 内に第2タブを開く)を素直に実装した範囲で、実機検証上は問題無く動いた
func TestResilience_MultiTab_SessionSharedAndLogoutPropagates(t *testing.T) {
	tab1 := newPage(t)
	if _, err := tab1.Goto(baseURL + "/tasks"); err != nil {
		t.Fatalf("tab1 Goto(/tasks)に失敗しました: %v", err)
	}
	loginViaLocal(t, tab1, false)

	// 同じブラウザコンテキスト(=同じCookie jar)から新しいタブを開く
	tab2, err := tab1.Context().NewPage()
	if err != nil {
		t.Fatalf("tab2の作成に失敗しました: %v", err)
	}
	if _, err := tab2.Goto(baseURL + "/tasks"); err != nil {
		t.Fatalf("tab2 Goto(/tasks)に失敗しました: %v", err)
	}
	if err := tab2.GetByTestId("task-list").WaitFor(); err != nil {
		t.Fatalf("tab2(同じブラウザの新タブ)がログイン済み扱いにならなかった: %v", err)
	}

	// tab1でログアウト(bff側のRedisセッションが削除される)
	if err := tab1.GetByTestId("logout-button").Click(); err != nil {
		t.Fatalf("tab1でのログアウトのクリックに失敗しました: %v", err)
	}
	if err := tab1.GetByTestId("login-email-input").WaitFor(); err != nil {
		t.Fatalf("tab1でのログアウト後にログイン画面が表示されなかった: %v", err)
	}

	// tab2はリロードして初めてサーバー側のセッション失効に気づく
	if _, err := tab2.Reload(); err != nil {
		t.Fatalf("tab2のReload()に失敗しました: %v", err)
	}
	if err := tab2.GetByTestId("login-email-input").WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(15000),
	}); err != nil {
		t.Fatalf("tab1でのログアウト後、tab2がログイン画面に戻らなかった: %v", err)
	}
}

// TestResilience_PasskeyRegistration_DoesNotBreakPasswordLogin は
// パスキー機能追加(auth_mode="passkey"分岐等の共通コード変更)が、既存のパスワードログイン経路を
// 壊していないことを、「パスキー登録済み」の状態から改めて確認する回帰テスト
func TestResilience_PasskeyRegistration_DoesNotBreakPasswordLogin(t *testing.T) {
	page := newPage(t)

	// passkey_test.goと同じ生CDPコマンドで仮想認証器を有効化(playwright-goには
	// WebAuthn専用の高レベルAPIが無いため)
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

	if _, err := page.Goto(baseURL + "/tasks"); err != nil {
		t.Fatalf("Goto(/tasks)に失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	if _, err := page.Goto(baseURL + "/account"); err != nil {
		t.Fatalf("Goto(/account)に失敗しました: %v", err)
	}
	if err := page.GetByTestId("passkey-register-button").Click(); err != nil {
		t.Fatalf("passkey-register-buttonのクリックに失敗しました: %v", err)
	}
	if err := page.GetByTestId("passkey-register-success").WaitFor(); err != nil {
		t.Fatalf("パスキー登録に失敗しました: %v", err)
	}

	if err := page.GetByTestId("logout-button").Click(); err != nil {
		t.Fatalf("logout-buttonのクリックに失敗しました: %v", err)
	}
	if err := page.GetByTestId("login-email-input").WaitFor(); err != nil {
		t.Fatalf("ログアウト後にログイン画面が表示されなかった: %v", err)
	}

	// ここが本題: パスキーボタンではなく、通常通りメールアドレス+パスワードでログインし直す
	loginViaLocal(t, page, false)
	if err := page.GetByTestId("task-list").WaitFor(); err != nil {
		t.Fatalf("パスキー登録後のパスワードログインでtask-listが表示されなかった: %v", err)
	}
}
