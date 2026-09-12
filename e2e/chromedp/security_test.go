// security_test.go は e2e/playwright/tests/security.spec.ts の4シナリオをchromedpへ移植したもの
// (3回目のe2e監査、セキュリティ観点)。
package e2e

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// countAlertDialogs はwindow.alert()由来のダイアログ(type=alert)だけをカウントする
// リスナーを登録する。acceptDialogs(既存)は種類を問わず全ダイアログを自動許可するため、
// 削除確認(window.confirm、type=confirm)と混同せずに「alertが実際に発火したか」だけを
// 見分けるために別立てにした(playwright版のdialogCountと同じ狙い)。
func countAlertDialogs(ctx context.Context) *int32 {
	var count int32
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if e, ok := ev.(*page.EventJavascriptDialogOpening); ok && e.Type == page.DialogTypeAlert {
			atomic.AddInt32(&count, 1)
		}
	})
	return &count
}

func TestSecurity_TamperedSessionCookie_Returns401(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	// 【なぜCDPのNetworkドメインが必要か】session_idはHttpOnly Cookieのため、
	// document.cookie経由では読み書きできない(意図的な設計、CONTRACT.mdセクション2)。
	// CDPはブラウザ特権でCookieストアへ直接アクセスできるため、chromedp/go-rodのような
	// CDPベースのツールでのみ「HttpOnly Cookieの改ざん」を模擬できる
	// (SeleniumはWebDriver標準のCookie APIで同じことができるが、これもブラウザ特権のAPI)
	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cks, err := network.GetCookies().WithURLs([]string{frontendBaseURL()}).Do(ctx)
		cookies = cks
		return err
	})); err != nil {
		t.Fatalf("Cookie取得に失敗: %v", err)
	}
	var sessionCookie *network.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session_id Cookieが見つからない")
	}

	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetCookie(sessionCookie.Name, "tampered-fake-session-id").
			WithURL(frontendBaseURL()).
			WithPath(sessionCookie.Path).
			Do(ctx)
	})); err != nil {
		t.Fatalf("Cookie書き換えに失敗: %v", err)
	}

	var status int64
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(async () => { const r = await fetch("/api/tasks", { credentials: "include" }); return r.status; })()`,
		&status,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) },
	)); err != nil {
		t.Fatalf("fetch実行に失敗: %v", err)
	}
	if status != 401 {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestSecurity_MissingCSRFHeader_Returns403(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	// 【なぜJSON文字列を1回だけ受け取るか】chromedp.EvaluateはGoの単一の型にしか
	// バインドできないため、ステータスコードとエラーメッセージをJS側でJSON文字列に
	// まとめてから、Go側でencoding/jsonへ渡す(JS↔Goの往復を1回で済ませる)
	var raw string
	js := `(async () => {
		const res = await fetch("/api/tasks", {
			method: "POST",
			credentials: "include",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ name: "csrf-test", status: "waiting", finished_on: "2030-01-01", label_ids: [] }),
		});
		const body = await res.json();
		return JSON.stringify({ status: res.status, error: body.error || "" });
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) },
	)); err != nil {
		t.Fatalf("fetch実行に失敗: %v", err)
	}

	var result struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("結果パースに失敗: %v (raw=%s)", err, raw)
	}
	if result.Status != 403 {
		t.Errorf("status = %d, want 403", result.Status)
	}
	if !strings.Contains(result.Error, "csrf") {
		t.Errorf("error = %q, want to contain %q", result.Error, "csrf")
	}
}

func TestSecurity_XSSPayloadInTaskName_NotExecuted(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)
	alertCount := countAlertDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	payload := "<script>x</script>"

	// 前回失敗分の残骸(同名行)があれば先に消しておく(playwright版と同じ理由、
	// Task.nameの20文字制限によりペイロードを一意化できないため)
	for {
		var exists bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`!!document.querySelector('[data-testid="task-row"][data-task-name="`+payload+`"]')`,
			&exists,
		)); err != nil {
			t.Fatalf("残骸確認に失敗: %v", err)
		}
		if !exists {
			break
		}
		if err := chromedp.Run(ctx, chromedp.Click(taskRowButtonSelector(payload, "task-delete-button"), chromedp.ByQuery)); err != nil {
			t.Fatalf("残骸削除に失敗: %v", err)
		}
		if err := waitTaskRowGone(ctx, payload); err != nil {
			t.Fatalf("残骸削除の完了待ちに失敗: %v", err)
		}
	}
	// 残骸削除で発生したdialogは本題(alert未発火の確認)に無関係なのでリセットする
	atomic.StoreInt32(alertCount, 0)

	err := chromedp.Run(ctx,
		chromedp.WaitVisible(`[data-testid="task-name-input"]`, chromedp.ByQuery),
		chromedp.SendKeys(`[data-testid="task-name-input"]`, payload, chromedp.ByQuery),
		setReactValue(`[data-testid="task-status-select"]`, "waiting"),
		setReactValue(`[data-testid="task-finished-on-input"]`, "2030-01-01"),
		chromedp.Click(`[data-testid="task-submit-button"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("タスク作成に失敗: %v", err)
	}
	if err := waitTaskRowVisible(ctx, payload); err != nil {
		t.Fatalf("作成したタスクが一覧に表示されない: %v", err)
	}

	// alertが一度も発火していないこと(XSSが実行されていないこと)
	if got := atomic.LoadInt32(alertCount); got != 0 {
		t.Errorf("alertダイアログが%d回発火した(XSSが実行された可能性)", got)
	}

	// DOM上に実際の<script>要素が挿入されていないこと
	var scriptElementCount int64
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-testid="task-row"][data-task-name="`+payload+`"]').querySelectorAll("script").length`,
		&scriptElementCount,
	)); err != nil {
		t.Fatalf("script要素の確認に失敗: %v", err)
	}
	if scriptElementCount != 0 {
		t.Errorf("script要素が%d個DOMに存在する(実際にHTMLとして解釈された)", scriptElementCount)
	}

	// 後片付け
	if err := chromedp.Run(ctx, chromedp.Click(taskRowButtonSelector(payload, "task-delete-button"), chromedp.ByQuery)); err != nil {
		t.Fatalf("削除に失敗: %v", err)
	}
	if err := waitTaskRowGone(ctx, payload); err != nil {
		t.Fatalf("削除後もタスクが一覧に残っている: %v", err)
	}
}

func TestSecurity_AfterLogout_TasksPageShowsLoginNotStaleData(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal() error = %v", err)
	}

	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-testid="logout-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("ログアウトに失敗: %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("再訪Navigate() error = %v", err)
	}
	if err := pollTestIDVisible(ctx, "login-email-input", 10*time.Second); err != nil {
		t.Fatalf("ログイン画面へ戻らなかった: %v", err)
	}
	var taskListExists bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('[data-testid="task-list"]')`, &taskListExists,
	)); err != nil {
		t.Fatalf("task-list確認に失敗: %v", err)
	}
	if taskListExists {
		t.Error("ログアウト後もtask-listが表示されている(情報露出)")
	}
}
