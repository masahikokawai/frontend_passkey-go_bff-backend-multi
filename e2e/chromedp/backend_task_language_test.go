package e2e

import (
	"context"
	"database/sql"
	"net"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// setTaskLanguage はMySQLへ直接接続し、多値Feature Flag `backend.task-language`
// (CONTRACT.mdセクション20)の default_variation を書き換える。setTaskCreateUX
// (feature_flag_helpers.go)と全く同じ方式・同じ「テスト終了時に必ず元へ戻す」規約に従う。
func setTaskLanguage(t *testing.T, variation string) {
	t.Helper()

	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil {
		t.Fatalf("MySQL接続に失敗しました: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var before string
	if err := db.QueryRow(`SELECT default_variation FROM feature_flags WHERE flag_key = 'backend.task-language'`).Scan(&before); err != nil {
		t.Fatalf("backend.task-languageの現在値取得に失敗しました: %v", err)
	}

	if _, err := db.Exec(`UPDATE feature_flags SET default_variation = ? WHERE flag_key = 'backend.task-language'`, variation); err != nil {
		t.Fatalf("backend.task-languageの更新に失敗しました: %v", err)
	}

	t.Cleanup(func() {
		if _, err := db.Exec(`UPDATE feature_flags SET default_variation = ? WHERE flag_key = 'backend.task-language'`, before); err != nil {
			t.Errorf("backend.task-languageを元の値(%s)へ戻すのに失敗しました: %v", before, err)
		}
	})

	waitForFeatureFlagPropagation()
}

// skipUnlessBackendRustRunning はbackend-rustの内部REST(既定:8093、CONTRACT.mdセクション20.9)へ
// TCP接続できるかだけを確認する。backend-rustは「追加構成」であり既定では起動していないため
// (CONTRACT.md・ランブック参照)、このテストだけがdocker-compose+コア構成では動かず
// 環境不足でこけるのを避け、未起動ならスキップする(このプロジェクトのe2eには他に
// 「オプションの追加プロセス」に依存するテストが無く、この規約は本テストで新設する)。
func skipUnlessBackendRustRunning(t *testing.T) {
	t.Helper()
	addr := envOr("BACKEND_RUST_REST_ADDR", "127.0.0.1:8093")
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Skipf("backend-rust(%s)に接続できないためスキップします(追加構成、既定では未起動。CONTRACT.mdセクション20.9参照): %v", addr, err)
	}
	_ = conn.Close()
}

// TestTaskCRUD_BackendTaskLanguageRust はCONTRACT.mdセクション20(backend多言語比較)の
// 「backend.task-languageをrustに切り替えても、Task CRUDがGoと同じ挙動で動く」ことを
// 検証する。これまでランブックの手順に沿ったcurl+目視でのみ確認されており、
// 自動テストが1つも無かった(e2e/以下でtask-language/task_languageを検索してもヒット無し)。
func TestTaskCRUD_BackendTaskLanguageRust(t *testing.T) {
	skipUnlessBackendRustRunning(t)
	setTaskLanguage(t, "rust")

	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()
	acceptDialogs(ctx)

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaKeycloak(ctx); err != nil {
		t.Fatalf("loginViaKeycloak() error = %v", err)
	}

	name := uniqueTaskName("RUST")
	updatedName := name + "-upd"

	// 作成(TestTaskCRUD(task_crud_test.go)と全く同じ操作、backendの実装言語だけが違う)
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
		t.Fatalf("作成したタスクが一覧に表示されない(backend.task-language=rust): %v", err)
	}

	// 更新
	err = chromedp.Run(ctx,
		chromedp.Click(taskRowButtonSelector(name, "task-edit-button"), chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-name-input"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("編集ボタンのクリックに失敗: %v", err)
	}
	err = chromedp.Run(ctx,
		setReactValue(`[data-testid="task-name-input"]`, updatedName),
		chromedp.Click(`[data-testid="task-submit-button"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("タスク更新の入力に失敗: %v", err)
	}
	if err := waitTaskRowVisible(ctx, updatedName); err != nil {
		t.Fatalf("更新後のタスクが一覧に表示されない(backend.task-language=rust): %v", err)
	}

	// 削除
	if err := chromedp.Run(ctx, chromedp.Click(taskRowButtonSelector(updatedName, "task-delete-button"), chromedp.ByQuery)); err != nil {
		t.Fatalf("削除ボタンのクリックに失敗: %v", err)
	}
	if err := waitTaskRowGone(ctx, updatedName); err != nil {
		t.Fatalf("削除後もタスクが一覧に残っている(backend.task-language=rust): %v", err)
	}
}
