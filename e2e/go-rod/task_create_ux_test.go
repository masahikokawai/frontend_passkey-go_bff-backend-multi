package e2e_go_rod

import (
	"testing"
	"time"
)

// TestTaskCreate_ModalUX は CONTRACT.md セクション19(Task登録UXの3パターン)の
// modal版シナリオ(chromedp版 task_create_ux_test.go と同じシナリオ)
func TestTaskCreate_ModalUX(t *testing.T) {
	setTaskCreateUX(t, "modal")

	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaKeycloak(page)

	name := uniqueTaskName("MDL")

	page.MustElement(testid("task-create-button")).MustWaitVisible().MustClick()
	page.MustElement(testid("task-create-modal")).MustWaitVisible()
	page.MustElement(testid("task-name-input")).MustWaitVisible().MustInput(name)
	setValueViaJS(page.MustElement(testid("task-status-select")), "waiting")
	setValueViaJS(page.MustElement(testid("task-finished-on-input")), "2030-01-01")
	page.MustElement(testid("task-submit-button")).MustClick()

	row := waitRowWithText(t, page, name)
	if row == nil {
		t.Fatalf("モーダルで作成したタスク %q が一覧に見つからない", name)
	}

	// 送信成功後はモーダルが自動的に閉じる設計(CONTRACT.mdセクション19.5)
	if els := page.MustElements(testid("task-create-modal")); len(els) != 0 {
		t.Error("タスク作成成功後もモーダルが開いたままになっている")
	}

	// 後片付け
	wait, handle := page.MustHandleDialog()
	go row.MustElement(testid("task-delete-button")).MustClick()
	wait()
	handle(true, "")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if findRowWithText(page, name) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("削除したはずのタスク %q が一覧にまだ存在する", name)
}

// TestTaskCreate_PageUX は同セクションのpage版シナリオ。「一覧へ戻る」導線の確認も含む
func TestTaskCreate_PageUX(t *testing.T) {
	setTaskCreateUX(t, "page")

	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaKeycloak(page)

	// 「一覧へ戻る」導線が機能することを先に確認する
	page.MustElement(testid("task-create-link")).MustWaitVisible().MustClick()
	page.MustElement(testid("task-form-page")).MustWaitVisible()
	page.MustElement(testid("back-to-tasks-link")).MustWaitVisible().MustClick()
	page.MustElement(testid("task-create-link")).MustWaitVisible()

	name := uniqueTaskName("PG")

	page.MustElement(testid("task-create-link")).MustClick()
	page.MustElement(testid("task-form-page")).MustWaitVisible()
	page.MustElement(testid("task-name-input")).MustWaitVisible().MustInput(name)
	setValueViaJS(page.MustElement(testid("task-status-select")), "waiting")
	setValueViaJS(page.MustElement(testid("task-finished-on-input")), "2030-01-01")
	page.MustElement(testid("task-submit-button")).MustClick()

	// 保存成功後は自動的に/tasksへ戻る(task-create-linkが再び見える)
	page.MustElement(testid("task-create-link")).MustWaitVisible()

	row := waitRowWithText(t, page, name)
	if row == nil {
		t.Fatalf("別ページで作成したタスク %q が一覧に見つからない", name)
	}

	// 後片付け
	wait, handle := page.MustHandleDialog()
	go row.MustElement(testid("task-delete-button")).MustClick()
	wait()
	handle(true, "")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if findRowWithText(page, name) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("削除したはずのタスク %q が一覧にまだ存在する", name)
}
