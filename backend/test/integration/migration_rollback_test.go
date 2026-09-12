//go:build integration

// 【テスト監査で発見】backend/migrations/配下の各ペア(up.sql/down.sql)について、
// down.sqlが実際にup.sqlを正しく巻き戻せるかを検証したテスト・CI設定がこれまで一切存在しなかった
// (cmd/migrateのdownサブコマンド自体はあるが、実際に流したことがあるかは別問題)
// down.sqlが壊れていても本番のロールバック作業の瞬間まで気づけない、というのが最悪のパターンのため、
// 全マイグレーションをup→down(全段階)→up の順に流し、途中でエラーが出ないことを確認する
//
// 【安全上の重要な注意】TEST_DB_DSNが指す先(README記載の例ではbff_gin_development、
// つまり開発時に使う共有DBそのもの)を直接down/upし直すと、ユーザーが手動検証で
// 作成したデータ・テーブルが全て消えてしまう。そのため、このテストはTEST_DB_DSNから
// 接続情報(ホスト/ポート/ユーザー)だけを借りて、専用の使い捨てデータベース
// (migrationRollbackTestDBName)を自分で作成・破棄し、共有DBのスキーマには一切触れない
package integration

import (
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const migrationRollbackTestDBName = "bff_gin_migration_rollback_test"

// dsnWithoutDBName は "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true" のような
// DSNから database名部分だけを取り除く("/bff_gin_development" → "/")
// CREATE DATABASE/DROP DATABASE自体は特定のDBに接続しなくても実行できるため、
// これで「サーバーには接続するが、共有DBのスキーマは一切参照しない」接続を作れる
var dsnDBNameRe = regexp.MustCompile(`/[^/?]+(\?|$)`)

func dsnWithoutDBName(dsn string) string {
	return dsnDBNameRe.ReplaceAllString(dsn, "/$1")
}

func dsnWithDBName(dsn, dbName string) string {
	return dsnDBNameRe.ReplaceAllString(dsn, "/"+dbName+"$1")
}

// countUpMigrationFiles は migrations/*.up.sql の数を数える(down段数の見積もりに使う。
// ハードコードした件数を都度更新する手間を避けるため実ファイルを数える)
func countUpMigrationFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatalf("migrationsディレクトリの読み取りに失敗: %v", err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			count++
		}
	}
	return count
}

func TestMigrations_AllDownStepsReverseCleanly(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	serverDSN := dsnWithoutDBName(dsn)
	adminDB, err := sql.Open("mysql", serverDSN)
	if err != nil {
		t.Fatalf("サーバーへの接続に失敗: %v", err)
	}
	// t.Cleanupは登録した順とは逆順(LIFO)に実行される。Close()を先に登録することで、
	// 後で登録するDROP DATABASE用のCleanupの方が先に(adminDBがまだ開いた状態で)実行される
	// (plainなdeferだとtest関数の return 時点で即座に閉じてしまい、Cleanup内での
	// adminDB使用時に"sql: database is closed"になる。実際にこの順序ミスで一度失敗した)
	t.Cleanup(func() { adminDB.Close() })

	// 既存の同名DBが万一残っていても、共有DBではなくこの使い捨てDBだけを作り直す
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + migrationRollbackTestDBName); err != nil {
		t.Fatalf("使い捨てDBの事前クリーンアップに失敗: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + migrationRollbackTestDBName + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("使い捨てDBの作成に失敗: %v", err)
	}
	t.Cleanup(func() {
		// テスト終了後は必ず破棄する(このDBは他の何にも使われていない使い捨て専用)
		if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + migrationRollbackTestDBName); err != nil {
			t.Logf("使い捨てDBの後始末に失敗(手動でDROP DATABASE %sしてください): %v", migrationRollbackTestDBName, err)
		}
	})

	testDSN := dsnWithDBName(dsn, migrationRollbackTestDBName)
	m, err := migrate.New("file://../../migrations", "mysql://"+testDSN)
	if err != nil {
		t.Fatalf("マイグレーション初期化に失敗: %v", err)
	}
	defer m.Close()

	// 全マイグレーションを最新まで適用する(000001〜000015、seedを含む)
	if err := m.Up(); err != nil {
		t.Fatalf("Up()に失敗: %v", err)
	}
	version, dirty, err := m.Version()
	if err != nil {
		t.Fatalf("Version()取得に失敗: %v", err)
	}
	if dirty {
		t.Fatalf("Up()直後にdirty状態になっている(version=%d)", version)
	}
	t.Logf("Up()完了。最新version=%d", version)

	// 1段ずつdownし、都度エラーが無いことを確認する(cmd/migrateのdownサブコマンドと同じ
	// Steps(-1)を繰り返す。全段まとめてm.Down()すると1つ目のdown.sqlで壊れていても
	// どのステップが原因か分かりにくいため、あえて1段ずつ実行する)
	//
	// 【実装時に判明】version=1(最初のマイグレーション)まで下がりきった状態でさらに
	// Steps(-1)を呼ぶと、golang-migrateはmigrate.ErrNoChangeではなく
	// "file does not exist"(存在しない"1つ前"のマイグレーションファイルを探そうとするため)
	// を返す。ErrNoChangeで終了判定するのではなく、migrations/配下の実ファイル数だけ
	// 正確にループする方式にする
	totalMigrations := countUpMigrationFiles(t)
	for step := 1; step <= totalMigrations; step++ {
		if err := m.Steps(-1); err != nil {
			t.Fatalf("%d段目(全%d段中)のdownで失敗(down.sqlが壊れている可能性): %v", step, totalMigrations, err)
		}
	}
	t.Logf("%d段のdownが全て成功", totalMigrations)

	version, dirty, err = m.Version()
	if err != migrate.ErrNilVersion {
		t.Fatalf("全段downした後もVersion()がErrNilVersionを返さない(version=%d, dirty=%v, err=%v)", version, dirty, err)
	}

	// down.sqlが本当に「正しく」巻き戻せているなら、再度up()した際にも
	// (テーブルが既に存在する等の)衝突エラーが起きないはず
	if err := m.Up(); err != nil {
		t.Fatalf("down後の再Up()に失敗(down.sqlが不完全にしか巻き戻せていない可能性): %v", err)
	}
	version, dirty, err = m.Version()
	if err != nil {
		t.Fatalf("再Up()後のVersion()取得に失敗: %v", err)
	}
	if dirty {
		t.Fatalf("再Up()後にdirty状態になっている(version=%d)", version)
	}
	t.Logf("再Up()も成功。全%d段のdown.sqlが正しくup.sqlを巻き戻せることを確認した", totalMigrations)

	// down.sqlに実データ喪失を伴うDDL(DROP TABLE/DROP COLUMN)が含まれる関係上、
	// このテスト自体が意図せず共有DBに影響しないことの最終確認として、
	// 使い捨てDB名がまさかの本番/開発DB名と衝突していないことも明示的に確認しておく
	if migrationRollbackTestDBName == "bff_gin_development" || migrationRollbackTestDBName == "bff_gin_test" {
		t.Fatalf("使い捨てDB名が共有DBと衝突している設定ミス: %s", migrationRollbackTestDBName)
	}
}
