package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestTaskCRUD は
// e2e/playwright/tests/task-crud.spec.ts と同じシナリオ
// (作成→一覧に反映→更新→一覧に反映→削除→一覧から消える)をchromedpで実装したもの。
func TestTaskCRUD(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaKeycloak(ctx); err != nil {
		t.Fatalf("loginViaKeycloak() error = %v", err)
	}

	name := uniqueTaskName("E2E")
	updatedName := name + "-upd"

	// 作成(/tasks上部に常設されたフォームへ直接入力する。別画面遷移はない)
	// ステータスselect・期限date inputはReactの制御されたinputのため、単純な.value代入では
	// onChangeが発火しない(helpers.goのsetReactValueコメント参照)。SendKeysで打鍵する
	// 名前inputとは異なり、setReactValueで直接値を設定する。
	err := chromedp.Run(ctx,
		chromedp.SendKeys(`[data-testid="task-name-input"]`, name, chromedp.ByQuery),
		setReactValue(`[data-testid="task-status-select"]`, "waiting"),
		setReactValue(`[data-testid="task-finished-on-input"]`, "2030-01-01"),
		chromedp.Click(`[data-testid="task-submit-button"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("タスク作成の入力に失敗: %v", err)
	}
	if err := waitTaskRowVisible(ctx, name); err != nil {
		t.Fatalf("作成したタスクが一覧に表示されない: %v", err)
	}

	// 更新(行の編集ボタンを押すと上部フォームに値が入る。別画面遷移はない)
	err = chromedp.Run(ctx,
		chromedp.Click(taskRowButtonSelector(name, "task-edit-button"), chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-name-input"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("編集ボタンのクリックに失敗: %v", err)
	}
	err = chromedp.Run(ctx,
		// 【実機検証で判明】chromedp.SetValue(DOM属性を直接書き換える低レベルAPI)は
		// "could not set value on node"で失敗する。代替のchromedp.Clearも試したが、
		// こちらは値を実際にはクリアしない(Reactの内部stateを経由しないため、
		// クリア後に読み取ってもDOM上の表示値が変わらないままだった)まま成功扱いになり、
		// 直後のSendKeysが既存値の末尾に追記されて壊れた文字列になっていた
		// (このプロジェクトのSelenium e2e実装で既知の"controlled input"問題と同種)。
		// setReactValue(setReactControlledValueJS)はReactのプロトタイプ側setterを直接呼び、
		// input/changeイベントを明示的に発火させるため、値の置き換え・クリア両方で確実に動く
		setReactValue(`[data-testid="task-name-input"]`, updatedName),
		chromedp.Click(`[data-testid="task-submit-button"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("タスク更新の入力に失敗: %v", err)
	}
	if err := waitTaskRowVisible(ctx, updatedName); err != nil {
		t.Fatalf("更新後のタスクが一覧に表示されない: %v", err)
	}

	// 削除(確認ダイアログはacceptDialogsで即時許可済み)
	if err := chromedp.Run(ctx, chromedp.Click(taskRowButtonSelector(updatedName, "task-delete-button"), chromedp.ByQuery)); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗: %v", err)
	}
	if err := waitTaskRowGone(ctx, updatedName); err != nil {
		t.Fatalf("削除後もタスクが一覧に残っている: %v", err)
	}
}

// taskRowButtonSelector は `data-task-name` 属性でtask-rowを特定し、その中の
// 指定testidのボタンを一意に選ぶCSSセレクタを組み立てる
// (playwright版の `getByTestId("task-row").filter({hasText}).getByTestId(...)` に相当)。
func taskRowButtonSelector(taskName, buttonTestID string) string {
	return `[data-testid="task-row"][data-task-name="` + taskName + `"] [data-testid="` + buttonTestID + `"]`
}

func waitTaskRowVisible(ctx context.Context, taskName string) error {
	sel := `[data-testid="task-row"][data-task-name="` + taskName + `"]`
	return chromedp.Run(ctx, chromedp.WaitVisible(sel, chromedp.ByQuery))
}

func waitTaskRowGone(ctx context.Context, taskName string) error {
	sel := `[data-testid="task-row"][data-task-name="` + taskName + `"]`
	return chromedp.Run(ctx, chromedp.WaitNotPresent(sel, chromedp.ByQuery))
}
