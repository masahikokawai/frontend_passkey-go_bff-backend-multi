package e2e

import (
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// TestTaskCRUD はJS版task-crud.spec.tsの
// 「作成→一覧に反映→更新→一覧に反映→削除→一覧から消える」に対応する
func TestTaskCRUD(t *testing.T) {
	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaKeycloak(t, page)

	name := uniqueTaskName("E2E")
	updatedName := name + "-upd"

	// 作成(/tasks上部に常設されたフォームへ直接入力する、別画面遷移はない)
	if err := page.GetByTestId("task-name-input").Fill(name); err != nil {
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

	row := page.GetByTestId("task-row").Filter(playwright.LocatorFilterOptions{HasText: name})
	mustWaitVisible(t, row, "作成したタスクの行")

	// 更新(行の編集ボタンを押すと上部フォームに値が入る、別画面遷移はない)
	if err := row.GetByTestId("task-edit-button").Click(); err != nil {
		t.Fatalf("編集ボタンのクリックに失敗しました: %v", err)
	}
	nameInput := page.GetByTestId("task-name-input")
	if err := nameInput.Fill(""); err != nil {
		t.Fatalf("タスク名クリアに失敗しました: %v", err)
	}
	if err := nameInput.Fill(updatedName); err != nil {
		t.Fatalf("更新後のタスク名入力に失敗しました: %v", err)
	}
	if err := page.GetByTestId("task-submit-button").Click(); err != nil {
		t.Fatalf("更新ボタンのクリックに失敗しました: %v", err)
	}

	updatedRow := page.GetByTestId("task-row").Filter(playwright.LocatorFilterOptions{HasText: updatedName})
	mustWaitVisible(t, updatedRow, "更新後のタスク行")

	// 削除(確認ダイアログは即時許可する)
	page.OnDialog(func(dialog playwright.Dialog) {
		_ = dialog.Accept()
	})
	if err := updatedRow.GetByTestId("task-delete-button").Click(); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗しました: %v", err)
	}

	waitForRowGone(t, page, updatedName)
}

// waitForRowGone は該当タスク行(task-row)が0件になるまでポーリングする
// playwright-goには`@playwright/test`の`expect().toHaveCount(0)`に相当する
// 組み込みアサーションが無いため(それはJSのテストランナー側の機能であり、
// このコミュニティ製Goバインディングにはブラウザ操作APIしか含まれない)、手動でポーリングする必要がある
// これはJS版との実装上の明確な違いの1つ
func waitForRowGone(t *testing.T, page playwright.Page, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		count, err := page.GetByTestId("task-row").Filter(playwright.LocatorFilterOptions{HasText: text}).Count()
		if err != nil {
			t.Fatalf("削除後の件数取得に失敗しました: %v", err)
		}
		if count == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("削除後もtask-row(%s)が%d件残っている、want 0", text, count)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
