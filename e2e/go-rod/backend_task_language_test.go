package e2e_go_rod

import (
	"database/sql"
	"net"
	"testing"
	"time"
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

	// bff/backendのポーリング間隔(既定10秒、CONTRACT.mdセクション13)分だけ確実に待つ
	time.Sleep(12 * time.Second)
}

// skipUnlessBackendRustRunning はbackend-rustの内部REST(既定:8093、CONTRACT.mdセクション20.9)へ
// TCP接続できるかだけを確認する。backend-rustは「追加構成」であり既定では起動していないため、
// このテストだけがコア構成では動かず環境不足でこけるのを避け、未起動ならスキップする
// (e2e/chromedpに同名の規約を新設したのに合わせる)。
func skipUnlessBackendRustRunning(t *testing.T) {
	t.Helper()
	addr := getEnv("BACKEND_RUST_REST_ADDR", "127.0.0.1:8093")
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

	page := newPage(t)
	gotoPath(page, "/tasks")
	loginViaKeycloak(page)

	name := uniqueTaskName("RUST")
	updatedName := name + "-upd"

	// 作成(TestTaskCRUD(task_crud_test.go)と全く同じ操作、backendの実装言語だけが違う)
	page.MustElement(testid("task-name-input")).MustWaitVisible().MustInput(name)
	setValueViaJS(page.MustElement(testid("task-status-select")), "waiting")
	setValueViaJS(page.MustElement(testid("task-finished-on-input")), "2030-01-01")
	page.MustElement(testid("task-submit-button")).MustClick()

	row := waitRowWithText(t, page, name)
	if row == nil {
		t.Fatalf("作成したタスク %q が一覧に見つからない(backend.task-language=rust)", name)
	}

	// 更新
	row.MustElement(testid("task-edit-button")).MustClick()
	nameInput := page.MustElement(testid("task-name-input"))
	setValueViaJS(nameInput, updatedName)
	page.MustElement(testid("task-submit-button")).MustClick()

	updatedRow := waitRowWithText(t, page, updatedName)
	if updatedRow == nil {
		t.Fatalf("更新後のタスク %q が一覧に見つからない(backend.task-language=rust)", updatedName)
	}

	// 削除
	wait, handle := page.MustHandleDialog()
	go updatedRow.MustElement(testid("task-delete-button")).MustClick()
	wait()
	handle(true, "")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if findRowWithText(page, updatedName) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("削除したはずのタスク %q が一覧にまだ存在する(backend.task-language=rust)", updatedName)
}
