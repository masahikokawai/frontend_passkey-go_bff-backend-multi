package e2e_go_rod

// resilience_test.go は e2e/playwright/tests/resilience.spec.ts の4シナリオを
// go-rodで実装したもの(2回目のe2e監査で追加)。「壊れやすいのに見落とされがちな、
// アプリの土台部分の挙動」(ネットワーク遅延・ブラウザ操作・複数タブ・認証手段の後方互換)を対象にする。

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-rod/rod"
)

// delayTasksAPI は /api/tasks* へのリクエストにだけ人為的な遅延を上乗せする。
//
// 【なぜHijackRequestsか】go-rodはPlaywrightのpage.route()に相当する高レベルAPIとして
// page.HijackRequests()を持っており、chromedpのように生のCDP Fetchドメインを直接
// 組み立てる必要がない(go-rodがCDPのFetchドメインをラップしている)。
func delayTasksAPI(page *rod.Page, delay time.Duration) *rod.HijackRouter {
	router := page.HijackRequests()
	router.MustAdd("*/api/tasks*", func(h *rod.Hijack) {
		time.Sleep(delay)
		h.MustLoadResponse()
	})
	go router.Run()
	return router
}

func TestResilience_SlowNetwork_ShowsLoadingState(t *testing.T) {
	page := newPage(t)
	router := delayTasksAPI(page, 1500*time.Millisecond)
	defer func() { _ = router.Stop() }()

	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	// ログイン成功→リダイレクト後、再度/api/tasksを呼ぶ実装のため、遅延を維持したままリロードし、
	// その瞬間に「読み込み中...」が見えることを確認する(Playwright版と同じ考え方)
	page.MustReload()
	// テキストベースの検索は go-rod の ElementR(selector, jsRegex) を使う("*" = 任意のタグ)
	loadingEl, err := page.Timeout(5 * time.Second).ElementR("*", "読み込み中")
	if err != nil {
		t.Fatalf("「読み込み中...」が表示されなかった: %v", err)
	}
	if !loadingEl.MustVisible() {
		t.Fatal("「読み込み中...」要素は存在するが表示されていない")
	}
	if !page.Timeout(10 * time.Second).MustElement(testid("task-list")).MustWaitVisible().MustVisible() {
		t.Fatal("task-listが表示されなかった")
	}
}

func TestResilience_BrowserBack_PreservesAuthAndState(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	gotoPath(page, "/labels")

	// 【chromedp版と同じ注意点】/labelsへは実ナビゲーション(MustNavigate)で遷移しているため、
	// ブラウザバックで/tasksへ戻る際も実際にドキュメントが再読み込みされる。
	// go-rodのMustElement系はCDPのDOM.querySelector相当だが、chromedpと違い
	// go-rodは要素検索のたびに毎回ライブのDOMツリーへ問い合わせる実装のため、
	// 実機確認の範囲ではドキュメント差し替えをまたいでも素直に動いた(chromedpのような
	// 明示的なワークアラウンドは不要だった)。念のため十分なタイムアウトを設定する。
	page.MustEval(`() => window.history.back()`)
	if !page.Timeout(15 * time.Second).MustElement(testid("task-list")).MustWaitVisible().MustVisible() {
		t.Fatal("戻った後にtask-listが表示されなかった(再ログインを要求された可能性)")
	}
}

func TestResilience_Reload_KeepsSession(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	page.MustReload()
	if !page.Timeout(10 * time.Second).MustElement(testid("task-list")).MustWaitVisible().MustVisible() {
		t.Fatal("リロード後にtask-listが表示されなかった")
	}
	if page.MustHas(testid("login-email-input")) {
		t.Error("リロード後にログイン画面へ飛ばされている(login-email-inputが存在する)")
	}
}

// TestResilience_MultiTab_SessionShared は
// 「片方のタブでログインすると、同じCookieを共有するもう片方のタブでも認証済み扱いになる」ことを確認する。
func TestResilience_MultiTab_SessionShared(t *testing.T) {
	browser := rod.New().MustConnect()
	t.Cleanup(func() { _ = browser.Close() })

	tab1 := browser.MustPage(baseURL() + "/tasks").MustWaitLoad()
	loginViaLocal(tab1, false)

	tab2 := browser.MustPage(baseURL() + "/tasks").MustWaitLoad()
	defer tab2.MustClose()
	// 【実機検証で判明】2つ目のタブに対して`.Timeout(N).MustElement(...).MustWaitVisible()`と
	// チェーンすると、要素は実際には既に存在・表示されているにもかかわらず(MustHasでは
	// 即座にtrueが返る)、内部的にハングして戻ってこないことがあった(原因未特定)。
	// 素朴に`MustHas`をポーリングする形に置き換えることで安定して動くことを確認した。
	if err := pollHasTestID(tab2, "task-list", 10*time.Second); err != nil {
		t.Fatalf("tab2(同じブラウザの新タブ)がログイン済み扱いにならなかった: %v", err)
	}
}

// TestResilience_MultiTab_LogoutPropagation は本来
// 「片方のタブでログアウトすると、もう片方のタブは次のアクセスでログイン画面(または401)に戻る」ことを
// 確認する意図だったが、実機検証の結果、go-rod v0.116.2 + このプロジェクトの環境では
// 安定して動かせなかったため、意図的にスキップする。
//
// 【実機検証での試行錯誤の記録】tab1でログアウト操作(クリック+ログイン画面表示待ち)を終えた
// "直後"に、別ページオブジェクトであるtab2へどんなコマンド(MustReload/MustNavigate/
// Evaluateでのlocation.reload()/単純なfetch呼び出しのEvaluateさえも)を送っても、CDPの応答が
// 返らずOSソケットの読み取りでブロックしたまま戻ってこない現象を確認した。以下を全て試したが
// 再現し続けた: (1)Goのcontext経由のTimeout()(ブロッキングI/O readには効かない)、
// (2)リトライ、(3)tab1/tab2を同じ*rod.Browserではなく、同じブラウザプロセスへの独立した
// 2本のCDP接続(rod.New().ControlURL(sameURL).MustConnect()を2回)に分離、
// (4)UIの再描画を待つのではなくfetch('/api/me')の直接呼び出しに変更。
// go-rod自体のクライアント実装の問題か、ブラウザ側(Chrome)がこのプロジェクトのバージョンで
// 同一プロセスへの複数DevTools接続をどう扱うかに起因する問題かは切り分けられなかった。
// Playwright版・Selenium版・chromedp版では同種のシナリオが問題無く動いていることから、
// go-rod固有(またはこの環境固有)の制約と判断した。
func TestResilience_MultiTab_LogoutPropagation(t *testing.T) {
	t.Skip("go-rod v0.116.2でこの環境では未解決のCDPハングに当たるため見送り。詳細はこの関数のコメント、およびREADME参照")
}

// pollHasTestID は data-testid を持つ要素が現れるまで MustHas をポーリングする。
// 2つ目以降のタブに対する `.Timeout().MustElement().MustWaitVisible()` の不安定さを避けるための
// 素朴な代替手段(resilience_test.goのTestResilience_MultiTab...のコメント参照)。
func pollHasTestID(page *rod.Page, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if page.MustHas(testid(id)) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("data-testid=%q が%v以内に現れなかった", id, timeout)
}

// TestResilience_PasskeyRegistration_DoesNotBreakPasswordLogin は
// パスキー機能追加(auth_mode="passkey"分岐等の共通コード変更)が、既存のパスワードログイン経路を
// 壊していないことを、「パスキー登録済み」の状態から改めて確認する回帰テスト。
//
// 【なぜsystem Chromeを使うか】passkey_test.goのnewPageWithSystemChromeと同じ理由
// (go-rod既定のChromiumはWebAuthn Level 3のJSON直列化APIを持たず、パスキー機能検出に失敗する)。
func TestResilience_PasskeyRegistration_DoesNotBreakPasswordLogin(t *testing.T) {
	page := newPageWithSystemChrome(t)
	gotoPath(page, "/tasks")
	if err := enableVirtualAuthenticator(page); err != nil {
		t.Fatalf("enableVirtualAuthenticator() error = %v", err)
	}
	loginViaLocal(page, false)

	gotoPath(page, "/account")
	page.MustElement(testid("passkey-register-button")).MustWaitVisible().MustClick()
	if !page.MustElement(testid("passkey-register-success")).MustWaitVisible().MustVisible() {
		t.Fatal("パスキー登録に失敗した")
	}

	page.MustElement(testid("logout-button")).MustClick()
	waitLoginScreen(page, 15*time.Second)

	// ここが本題: パスキーボタンではなく、通常通りメールアドレス+パスワードでログインし直す
	loginViaLocal(page, false)
	if !page.MustElement(testid("task-list")).MustWaitVisible().MustVisible() {
		t.Fatal("パスキー登録後のパスワードログインでtask-listが表示されなかった")
	}
}
