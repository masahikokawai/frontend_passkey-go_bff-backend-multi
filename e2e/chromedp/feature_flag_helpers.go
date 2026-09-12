package e2e

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// waitForFeatureFlagPropagation はbff(HTTP retrieverでbackendをポーリング)の
// 既定ポーリング間隔(10秒)を確実に超えるまで待つ。backend自身のMySQL retrieverも
// 同程度の間隔でポーリングしているため、これで両方に反映される。
func waitForFeatureFlagPropagation() {
	time.Sleep(12 * time.Second)
}

// mysqlDSN は docker-compose.yaml の mysql サービス(ホスト公開ポート)に対応する既定値。
// backend/admin/go 等、他の全コンポーネントと同じ既定DSNを使う。
func mysqlDSN() string {
	return envOr("MYSQL_DSN", "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true")
}

// setTaskCreateUX はMySQLへ直接接続し、多値Feature Flag `frontend.task-create-ux`
// (CONTRACT.mdセクション19)の default_variation を書き換える。テスト終了時に元の値
// (このプロジェクトの既定値 "inline")へ必ず戻す(他のテスト・他のフォークの作業に
// 影響しないようにするため)。
//
// admin/go・admin/rails経由(HTTP)ではなく直接SQLで切り替えるのは、e2eテスト自身が
// 「事前に手動でDBを切り替えてから実行する」という運用(e2e/SELECTORS.md参照)を、
// テストの中で自己完結させ、CIでも手動操作なしに実行できるようにするため。
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

	// bff側のポーリング間隔(既定10秒、CONTRACT.mdセクション13)分だけ確実に反映を待つ。
	waitForFeatureFlagPropagation()
}
