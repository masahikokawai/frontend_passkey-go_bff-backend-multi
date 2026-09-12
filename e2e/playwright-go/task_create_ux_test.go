package e2e

import (
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// TestTaskCreate_ModalUX は CONTRACT.md セクション19(Task登録UXの3パターン)の
// modal版シナリオ(chromedp・go-rod版と同じシナリオ)
func TestTaskCreate_ModalUX(t *testing.T) {
	setTaskCreateUX(t, "modal")

	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaKeycloak(t, page)

	name := uniqueTaskName("MDL")

	createButton := page.GetByTestId("task-create-button")
	mustWaitVisible(t, createButton, "task-create-button")
	if err := createButton.Click(); err != nil {
		t.Fatalf("task-create-buttonのクリックに失敗しました: %v", err)
	}
	mustWaitVisible(t, page.GetByTestId("task-create-modal"), "task-create-modal")

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
	mustWaitVisible(t, row, "モーダルで作成したタスクの行")

	// 送信成功後はモーダルが自動的に閉じる設計(CONTRACT.mdセクション19.5)
	count, err := page.GetByTestId("task-create-modal").Count()
	if err != nil {
		t.Fatalf("モーダルの残存確認に失敗しました: %v", err)
	}
	if count != 0 {
		t.Error("タスク作成成功後もモーダルが開いたままになっている")
	}

	// 後片付け
	page.OnDialog(func(dialog playwright.Dialog) { _ = dialog.Accept() })
	if err := row.GetByTestId("task-delete-button").Click(); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗しました: %v", err)
	}
	waitForRowGone(t, page, name)
}

// TestTaskCreate_PageUX は同セクションのpage版シナリオ
// 「一覧へ戻る」導線の確認も含む
func TestTaskCreate_PageUX(t *testing.T) {
	setTaskCreateUX(t, "page")

	page := newPage(t)
	if _, err := page.Goto("/tasks"); err != nil {
		t.Fatalf("gotoに失敗しました: %v", err)
	}
	loginViaKeycloak(t, page)

	// 「一覧へ戻る」導線が機能することを先に確認する
	createLink := page.GetByTestId("task-create-link")
	mustWaitVisible(t, createLink, "task-create-link")
	if err := createLink.Click(); err != nil {
		t.Fatalf("task-create-linkのクリックに失敗しました: %v", err)
	}
	mustWaitVisible(t, page.GetByTestId("task-form-page"), "task-form-page")
	backLink := page.GetByTestId("back-to-tasks-link")
	mustWaitVisible(t, backLink, "back-to-tasks-link")
	if err := backLink.Click(); err != nil {
		t.Fatalf("back-to-tasks-linkのクリックに失敗しました: %v", err)
	}
	mustWaitVisible(t, page.GetByTestId("task-create-link"), "task-create-link(戻り確認)")

	name := uniqueTaskName("PG")

	if err := page.GetByTestId("task-create-link").Click(); err != nil {
		t.Fatalf("task-create-linkのクリックに失敗しました: %v", err)
	}
	mustWaitVisible(t, page.GetByTestId("task-form-page"), "task-form-page")

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

	// 保存成功後は自動的に/tasksへ戻る(task-create-linkが再び見える)
	mustWaitVisible(t, page.GetByTestId("task-create-link"), "task-create-link(保存後の戻り確認)")

	row := page.GetByTestId("task-row").Filter(playwright.LocatorFilterOptions{HasText: name})
	mustWaitVisible(t, row, "別ページで作成したタスクの行")

	// 後片付け
	page.OnDialog(func(dialog playwright.Dialog) { _ = dialog.Accept() })
	if err := row.GetByTestId("task-delete-button").Click(); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗しました: %v", err)
	}
	waitForRowGone(t, page, name)
}
