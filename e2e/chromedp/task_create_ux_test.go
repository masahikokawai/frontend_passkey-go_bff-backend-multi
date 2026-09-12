package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestTaskCreate_ModalUX は CONTRACT.md セクション19(Task登録UXの3パターン)の
// modal版シナリオ: frontend.task-create-ux=modal のとき、"タスクを登録"ボタン→
// モーダル内のフォームで作成→モーダルが閉じ一覧に反映されることを確認する。
func TestTaskCreate_ModalUX(t *testing.T) {
	setTaskCreateUX(t, "modal")

	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaKeycloak(ctx); err != nil {
		t.Fatalf("loginViaKeycloak() error = %v", err)
	}

	name := uniqueTaskName("MDL")

	err := chromedp.Run(ctx,
		chromedp.WaitVisible(`[data-testid="task-create-button"]`, chromedp.ByQuery),
		chromedp.Click(`[data-testid="task-create-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-create-modal"]`, chromedp.ByQuery),
		chromedp.SendKeys(`[data-testid="task-name-input"]`, name, chromedp.ByQuery),
		setReactValue(`[data-testid="task-status-select"]`, "waiting"),
		setReactValue(`[data-testid="task-finished-on-input"]`, "2030-01-01"),
		chromedp.Click(`[data-testid="task-submit-button"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("モーダルでのタスク作成に失敗: %v", err)
	}
	if err := waitTaskRowVisible(ctx, name); err != nil {
		t.Fatalf("作成したタスクが一覧に表示されない: %v", err)
	}

	// モーダルが閉じていること(送信成功で自動的に閉じる設計、CONTRACT.mdセクション19.5)も確認する
	var stillOpen bool
	if err := chromedp.Run(ctx, chromedp.EvaluateAsDevTools(
		`document.querySelector('[data-testid="task-create-modal"]') !== null`, &stillOpen,
	)); err != nil {
		t.Fatalf("モーダルの表示状態確認に失敗: %v", err)
	}
	if stillOpen {
		t.Error("タスク作成成功後もモーダルが開いたままになっている")
	}

	// 後片付け(他のテストへ影響しないよう削除しておく)
	if err := chromedp.Run(ctx, chromedp.Click(taskRowButtonSelector(name, "task-delete-button"), chromedp.ByQuery)); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗: %v", err)
	}
	if err := waitTaskRowGone(ctx, name); err != nil {
		t.Fatalf("削除後もタスクが一覧に残っている: %v", err)
	}
}

// TestTaskCreate_PageUX は同セクションのpage版シナリオ: frontend.task-create-ux=page のとき、
// "タスクを登録"リンク→/tasks/newの専用ページでフォームを送信→自動的に/tasksへ戻り一覧に反映、
// さらに"一覧へ戻る"リンク(back-to-tasks-link)が機能することを確認する。
func TestTaskCreate_PageUX(t *testing.T) {
	setTaskCreateUX(t, "page")

	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaKeycloak(ctx); err != nil {
		t.Fatalf("loginViaKeycloak() error = %v", err)
	}

	// "一覧へ戻る"導線が機能することを先に確認する(実機検証で「戻る導線が無い画面」の
	// 不具合が見つかった経緯があるため、この確認自体をシナリオに含める)。
	err := chromedp.Run(ctx,
		chromedp.WaitVisible(`[data-testid="task-create-link"]`, chromedp.ByQuery),
		chromedp.Click(`[data-testid="task-create-link"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-form-page"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="back-to-tasks-link"]`, chromedp.ByQuery),
		chromedp.Click(`[data-testid="back-to-tasks-link"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-create-link"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("「一覧へ戻る」導線の確認に失敗: %v", err)
	}

	name := uniqueTaskName("PG")

	err = chromedp.Run(ctx,
		chromedp.Click(`[data-testid="task-create-link"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-form-page"]`, chromedp.ByQuery),
		chromedp.SendKeys(`[data-testid="task-name-input"]`, name, chromedp.ByQuery),
		setReactValue(`[data-testid="task-status-select"]`, "waiting"),
		setReactValue(`[data-testid="task-finished-on-input"]`, "2030-01-01"),
		chromedp.Click(`[data-testid="task-submit-button"]`, chromedp.ByQuery),
		// 保存成功後は自動的に/tasksへ戻る設計(CONTRACT.mdセクション19.5)
		chromedp.WaitVisible(`[data-testid="task-create-link"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("別ページでのタスク作成に失敗: %v", err)
	}
	if err := waitTaskRowVisible(ctx, name); err != nil {
		t.Fatalf("作成したタスクが一覧に表示されない: %v", err)
	}

	// 後片付け
	if err := chromedp.Run(ctx, chromedp.Click(taskRowButtonSelector(name, "task-delete-button"), chromedp.ByQuery)); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗: %v", err)
	}
	if err := waitTaskRowGone(ctx, name); err != nil {
		t.Fatalf("削除後もタスクが一覧に残っている: %v", err)
	}
}
