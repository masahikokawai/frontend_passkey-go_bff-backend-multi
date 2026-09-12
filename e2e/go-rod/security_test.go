// security_test.go は e2e/playwright/tests/security.spec.ts の4シナリオをgo-rodへ移植したもの
// (3回目のe2e監査、セキュリティ観点)
package e2e_go_rod

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// countAlertDialogs はwindow.alert()由来のダイアログ(type=alert)だけをカウントするリスナーを、page.EachEventで持続的に登録する
// MustHandleDialog(既存、task_crud_test.go参照)は
// 「次の1回だけ」を待つワンショットAPIのため、削除確認(confirm)と混同せず
// 「作成〜確認までの間にalertが何回発火したか」を数えるにはEachEventが必要になる
// (chromedp版のListenTargetと同じ狙い)
func countAlertDialogs(page *rod.Page) (count *int32, stop func()) {
	var c int32
	wait := page.EachEvent(func(e *proto.PageJavascriptDialogOpening) {
		if e.Type == proto.PageDialogTypeAlert {
			atomic.AddInt32(&c, 1)
		}
		// alert/confirmいずれも即座に許可し、テストの進行をブロックしない
		_ = proto.PageHandleJavaScriptDialog{Accept: true}.Call(page)
	})
	go wait()
	return &c, func() {}
}

func TestSecurity_TamperedSessionCookie_Returns401(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	// 【なぜMustCookies/MustSetCookiesが必要か】
	// session_idはHttpOnly Cookieのため、document.cookie経由では読み書きできない(意図的な設計、CONTRACT.mdセクション2)
	// go-rodもCDPベースのため、chromedp版と同じくブラウザ特権のCookie APIで改ざんを模擬する
	cookies := page.MustCookies(baseURL())
	var sessionCookie *proto.NetworkCookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session_id Cookieが見つからない")
	}

	page.MustSetCookies(&proto.NetworkCookieParam{
		Name:  sessionCookie.Name,
		Value: "tampered-" + uniqueTaskName("x"),
		URL:   baseURL(),
		Path:  sessionCookie.Path,
	})

	// 【go-rod特有の注意】MustEvalに渡す文字列は「関数リテラル」であり、go-rodが内部で
	// これをapply()するため、Playwright/chromedp版のような即時実行IIFE( `(async()=>{})()` )
	// ではなく、末尾の`()`を付けない関数式のまま渡す必要がある(実機検証で判明: IIFEを渡すと
	// 「戻り値(Promise)に対してapplyしようとしてTypeErrorになる」という分かりにくい失敗をする)
	status := page.MustEval(`async () => {
		const r = await fetch("/api/tasks", { credentials: "include" });
		return r.status;
	}`).Int()
	if status != 401 {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestSecurity_MissingCSRFHeader_Returns403(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	result := page.MustEval(`async () => {
		const res = await fetch("/api/tasks", {
			method: "POST",
			credentials: "include",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ name: "csrf-test", status: "waiting", finished_on: "2030-01-01", label_ids: [] }),
		});
		const body = await res.json();
		return { status: res.status, error: body.error || "" };
	}`)

	status := result.Get("status").Int()
	errMsg := result.Get("error").String()
	if status != 403 {
		t.Errorf("status = %d, want 403", status)
	}
	if !strings.Contains(errMsg, "csrf") {
		t.Errorf("error = %q, want to contain %q", errMsg, "csrf")
	}
}

func TestSecurity_XSSPayloadInTaskName_NotExecuted(t *testing.T) {
	page := newPage(t)
	alertCount, stop := countAlertDialogs(page)
	defer stop()

	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	payload := "<script>x</script>"

	// 前回失敗分の残骸(同名行)があれば先に消しておく(playwright版と同じ理由)
	for {
		row := findRowWithText(page, "script")
		if row == nil {
			break
		}
		row.MustElement(testid("task-delete-button")).MustClick()
		time.Sleep(300 * time.Millisecond)
	}
	// 残骸削除で発生したdialogは本題(alert未発火の確認)に無関係なのでリセットする
	atomic.StoreInt32(alertCount, 0)

	page.MustElement(testid("task-name-input")).MustWaitVisible().MustInput(payload)
	setValueViaJS(page.MustElement(testid("task-status-select")), "waiting")
	setValueViaJS(page.MustElement(testid("task-finished-on-input")), "2030-01-01")
	page.MustElement(testid("task-submit-button")).MustClick()

	row := waitRowWithText(t, page, "script")
	if row == nil {
		t.Fatal("作成したタスクが一覧に見つからない")
	}

	if got := atomic.LoadInt32(alertCount); got != 0 {
		t.Errorf("alertダイアログが%d回発火した(XSSが実行された可能性)", got)
	}

	if !strings.Contains(row.MustText(), payload) {
		t.Errorf("行のテキストにペイロードがそのまま(エスケープされて)含まれていない: %q", row.MustText())
	}
	scriptElements := row.MustElements("script")
	if len(scriptElements) != 0 {
		t.Errorf("script要素が%d個DOMに存在する(実際にHTMLとして解釈された)", len(scriptElements))
	}

	// 後片付け
	row.MustElement(testid("task-delete-button")).MustClick()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if findRowWithText(page, "script") == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("削除したはずのタスクが一覧にまだ存在する")
}

func TestSecurity_AfterLogout_TasksPageShowsLoginNotStaleData(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaLocal(page, false)

	page.MustElement(testid("logout-button")).MustClick()
	waitLoginScreen(page, 15*time.Second)

	gotoPath(page, "/tasks")
	waitLoginScreen(page, 10*time.Second)

	taskLists := page.MustElements(testid("task-list"))
	if len(taskLists) != 0 {
		t.Error("ログアウト後もtask-listが表示されている(情報露出)")
	}
}
