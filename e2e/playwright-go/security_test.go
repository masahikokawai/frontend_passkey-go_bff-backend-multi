// security_test.go は e2e/playwright/tests/security.spec.ts の4シナリオをplaywright-goへ移植したもの
// (3回目のe2e監査、セキュリティ観点)
// playwright-go は本家 Playwright ドライバをそのまま操作する
// バインディングのため、JS版とほぼ1対1で書ける(cookie API・OnDialog・Evaluate の await 挙動もJS版と同じ)
package e2e

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

func TestSecurity_TamperedSessionCookie_Returns401(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	// 【なぜBrowserContext.Cookies/AddCookiesが必要か】session_idはHttpOnly Cookie のため、
	// document.cookie経由(page.Evaluate)では読み書きできない(意図的な設計、CONTRACT.mdセクション2)
	// BrowserContext の Cookie API はブラウザ特権で HttpOnly Cookie も扱える
	cookies, err := page.Context().Cookies()
	if err != nil {
		t.Fatalf("Cookie取得に失敗しました: %v", err)
	}
	var sessionCookie *playwright.Cookie
	for i := range cookies {
		if cookies[i].Name == "session_id" {
			sessionCookie = &cookies[i]
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session_id Cookieが見つからない")
	}

	// 【実機検証で判明】
	// playwright-goのAddCookiesは「URL」または「Domain+Path」の
	// どちらか一方のみを要求し、URL と Path を両方指定すると "Cookie should have either url or path" で拒否される
	// 既存Cookie の path を気にする必要は無く、frontend のオリジン(URL)だけ指定すれば十分
	tampered := "tampered-" + uniqueTaskName("x")
	if err := page.Context().AddCookies([]playwright.OptionalCookie{
		{
			Name:  "session_id",
			Value: tampered,
			URL:   playwright.String(baseURL),
		},
	}); err != nil {
		t.Fatalf("Cookie書き換えに失敗しました: %v", err)
	}

	result, err := page.Evaluate(`(async () => {
		const res = await fetch("/api/tasks", { credentials: "include" });
		return res.status;
	})()`)
	if err != nil {
		t.Fatalf("fetch実行に失敗しました: %v", err)
	}
	status, ok := result.(int)
	if !ok {
		// playwright-goのEvaluateはJSのnumberをfloat64で返すことがあるため両対応する
		if f, ok2 := result.(float64); ok2 {
			status = int(f)
		} else {
			t.Fatalf("戻り値の型が想定外です: %T (%v)", result, result)
		}
	}
	if status != 401 {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestSecurity_MissingCSRFHeader_Returns403(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	result, err := page.Evaluate(`(async () => {
		const res = await fetch("/api/tasks", {
			method: "POST",
			credentials: "include",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ name: "csrf-test", status: "waiting", finished_on: "2030-01-01", label_ids: [] }),
		});
		const body = await res.json();
		return { status: res.status, error: body.error || "" };
	})()`)
	if err != nil {
		t.Fatalf("fetch実行に失敗しました: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("戻り値の型が想定外です: %T (%v)", result, result)
	}
	status := toInt(m["status"])
	errMsg, _ := m["error"].(string)

	if status != 403 {
		t.Errorf("status = %d, want 403", status)
	}
	if !strings.Contains(errMsg, "csrf") {
		t.Errorf("error = %q, want to contain %q", errMsg, "csrf")
	}
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return -1
	}
}

func TestSecurity_XSSPayloadInTaskName_NotExecuted(t *testing.T) {
	page := newPage(t)

	// 【なぜOnDialogをこのタイミングで登録するか】JS版のpage.on("dialog", ...)と同じ、テスト全体で持続するリスナー
	// alert(type=alert)だけをカウントし、削除確認(confirm)とは区別する
	// 両方とも即時acceptしてブロックしないようにする
	var alertCount int32
	page.OnDialog(func(dialog playwright.Dialog) {
		if dialog.Type() == "alert" {
			atomic.AddInt32(&alertCount, 1)
		}
		_ = dialog.Accept()
	})

	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	payload := "<script>x</script>"

	// 前回失敗分の残骸(同名行)があれば先に消しておく(JS版と同じ理由)
	staleRows := page.GetByTestId("task-row").Filter(playwright.LocatorFilterOptions{HasText: "script"})
	staleCount, err := staleRows.Count()
	if err != nil {
		t.Fatalf("残骸件数取得に失敗しました: %v", err)
	}
	for i := 0; i < staleCount; i++ {
		if err := staleRows.First().GetByTestId("task-delete-button").Click(); err != nil {
			t.Fatalf("残骸削除に失敗しました: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	// 残骸削除で発生したdialogは本題(alert未発火の確認)に無関係なのでリセットする
	atomic.StoreInt32(&alertCount, 0)

	if err := page.GetByTestId("task-name-input").Fill(payload); err != nil {
		t.Fatalf("タスク名入力に失敗しました: %v", err)
	}
	if _, err := page.GetByTestId("task-status-select").SelectOption(playwright.SelectOptionValues{
		Values: playwright.StringSlice("waiting"),
	}); err != nil {
		t.Fatalf("ステータス選択に失敗しました: %v", err)
	}
	if err := page.GetByTestId("task-finished-on-input").Fill("2030-01-01"); err != nil {
		t.Fatalf("期限入力に失敗しました: %v", err)
	}
	if err := page.GetByTestId("task-submit-button").Click(); err != nil {
		t.Fatalf("作成ボタンのクリックに失敗しました: %v", err)
	}

	row := page.GetByTestId("task-row").Filter(playwright.LocatorFilterOptions{HasText: "script"})
	mustWaitVisible(t, row, "XSSペイロードを含むタスク行")

	if got := atomic.LoadInt32(&alertCount); got != 0 {
		t.Errorf("alertダイアログが%d回発火した(XSSが実行された可能性)", got)
	}

	text, err := row.TextContent()
	if err != nil {
		t.Fatalf("行のテキスト取得に失敗しました: %v", err)
	}
	if !strings.Contains(text, payload) {
		t.Errorf("行にペイロードがそのまま(エスケープされて)含まれていない: %q", text)
	}
	scriptCount, err := row.Locator("script").Count()
	if err != nil {
		t.Fatalf("script要素の確認に失敗しました: %v", err)
	}
	if scriptCount != 0 {
		t.Errorf("script要素が%d個DOMに存在する(実際にHTMLとして解釈された)", scriptCount)
	}

	// 後片付け
	if err := row.GetByTestId("task-delete-button").Click(); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗しました: %v", err)
	}
	waitForRowGone(t, page, "script")
}

func TestSecurity_AfterLogout_TasksPageShowsLoginNotStaleData(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaLocal(t, page, false)

	if err := page.GetByTestId("logout-button").Click(); err != nil {
		t.Fatalf("ログアウトボタンのクリックに失敗しました: %v", err)
	}
	waitForLoginScreen(t, page)

	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("再訪gotoに失敗しました: %v", err)
	}
	waitForLoginScreen(t, page)

	count, err := page.GetByTestId("task-list").Count()
	if err != nil {
		t.Fatalf("task-list件数取得に失敗しました: %v", err)
	}
	if count != 0 {
		t.Error("ログアウト後もtask-listが表示されている(情報露出)")
	}
}
