package e2e_go_rod

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func mysqlDSN() string {
	return getEnv("MYSQL_DSN", "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true")
}

// setTaskCreateUX はMySQLへ直接接続し、多値Feature Flag `frontend.task-create-ux`
// (CONTRACT.mdセクション19)の default_variation を書き換える
// テスト終了時に元の値(このプロジェクトの既定値 "inline")へ必ず戻す(chromedp版と同じ設計、e2e/chromedp/feature_flag_helpers.go参照)
func setTaskCreateUX(t *testing.T, variation string) {
	t.Helper()

	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil {
		t.Fatalf("MySQL接続に失敗しました: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var before string
	if err := db.QueryRow(`SELECT default_variation FROM feature_flags WHERE flag_key = 'frontend.task-create-ux'`).Scan(&before); err != nil {
		t.Fatalf("frontend.task-create-uxの現在値取得に失敗しました: %v", err)
	}

	if _, err := db.Exec(`UPDATE feature_flags SET default_variation = ? WHERE flag_key = 'frontend.task-create-ux'`, variation); err != nil {
		t.Fatalf("frontend.task-create-uxの更新に失敗しました: %v", err)
	}

	t.Cleanup(func() {
		if _, err := db.Exec(`UPDATE feature_flags SET default_variation = ? WHERE flag_key = 'frontend.task-create-ux'`, before); err != nil {
			t.Errorf("frontend.task-create-uxを元の値(%s)へ戻すのに失敗しました: %v", before, err)
		}
	})

	// bff/backendのポーリング間隔(既定10秒、CONTRACT.mdセクション13)分だけ確実に待つ
	time.Sleep(12 * time.Second)
}
