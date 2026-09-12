package e2e_go_rod

import (
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
)

// findRowWithText は現在のDOMの中から、指定したタスク名を含む task-row を探す
// (見つからなければnil。playwright版の page.getByTestId("task-row").filter({ hasText }) に相当)
func findRowWithText(page *rod.Page, text string) *rod.Element {
	for _, r := range page.MustElements(testid("task-row")) {
		if strings.Contains(r.MustText(), text) {
			return r
		}
	}
	return nil
}

// waitRowWithText は指定したタスク名を含む task-row が現れるまで短時間ポーリングする
func waitRowWithText(t *testing.T, page *rod.Page, text string) *rod.Element {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if row := findRowWithText(page, text); row != nil {
			return row
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// TestTaskCRUD は作成→一覧に反映→更新→一覧に反映→削除→一覧から消える、
// という一連のシナリオを検証する(playwright版 task-crud.spec.ts に対応)
func TestTaskCRUD(t *testing.T) {
	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaKeycloak(page)

	name := uniqueTaskName("E2E")
	updatedName := name + "-upd"

	// 作成(/tasks上部に常設されたフォームへ直接入力する。別画面遷移はない)
	page.MustElement(testid("task-name-input")).MustWaitVisible().MustInput(name)
	setValueViaJS(page.MustElement(testid("task-status-select")), "waiting")
	setValueViaJS(page.MustElement(testid("task-finished-on-input")), "2030-01-01")
	page.MustElement(testid("task-submit-button")).MustClick()

	row := waitRowWithText(t, page, name)
	if row == nil {
		t.Fatalf("作成したタスク %q が一覧に見つからない", name)
	}

	// 更新(行の編集ボタンを押すと上部フォームに値が入る。別画面遷移はない)
	row.MustElement(testid("task-edit-button")).MustClick()
	nameInput := page.MustElement(testid("task-name-input"))
	setValueViaJS(nameInput, updatedName)
	page.MustElement(testid("task-submit-button")).MustClick()

	updatedRow := waitRowWithText(t, page, updatedName)
	if updatedRow == nil {
		t.Fatalf("更新後のタスク %q が一覧に見つからない", updatedName)
	}

	// 削除(確認ダイアログは即時許可する)
	wait, handle := page.MustHandleDialog()
	go updatedRow.MustElement(testid("task-delete-button")).MustClick()
	wait()
	handle(true, "")

	// 削除完了(一覧再取得)を待ってから、消えたことを確認する
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if findRowWithText(page, updatedName) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("削除したはずのタスク %q が一覧にまだ存在する", updatedName)
}
