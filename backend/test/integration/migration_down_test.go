//go:build integration

// マイグレーションのdown.sqlが実際に正しくスキーマ・データを復元するかを検証する
//
// 【背景】
// これまでのテストは「migrate up後の最終スキーマ」に対する検証ばかりで、down.sql 自体(このプロジェクト固有のロジック
// golang-migrateライブラリの標準動作である「upを2回実行しても壊れない」等とは別の話)が正しいかは一度も検証されていなかった
// TEST_DB_DSN(共有の開発用DB)を直接dropしたりカラムを付け外ししたりするのは他プロセス(backend/bff等)への影響が大きすぎるため、
// 専用の一時データベースを作成・破棄して検証する
package integration

import (
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// tempDatabaseDSN はTEST_DB_DSNのデータベース名部分だけを一時DB名に差し替える
// 例: "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true"
//
//	→ "root@tcp(127.0.0.1:13306)/bff_gin_migration_down_test?parseTime=true"
var dbNamePattern = regexp.MustCompile(`/([A-Za-z0-9_]+)(\?|$)`)

func tempDatabaseDSN(t *testing.T, baseDSN, tempDBName string) string {
	t.Helper()
	if !dbNamePattern.MatchString(baseDSN) {
		t.Fatalf("TEST_DB_DSNからデータベース名を抽出できません: %s", baseDSN)
	}
	return dbNamePattern.ReplaceAllString(baseDSN, "/"+tempDBName+"$2")
}

func TestMigrationDown_RestoresSchemaAndData(t *testing.T) {
	baseDSN := os.Getenv("TEST_DB_DSN")
	if baseDSN == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	const tempDBName = "bff_gin_migration_down_test"
	tempDSN := tempDatabaseDSN(t, baseDSN, tempDBName)

	// 一時データベースを作成(既存の共有DBには一切触れない)
	adminDB, err := sql.Open("mysql", dbNamePattern.ReplaceAllString(baseDSN, "/$2"))
	if err != nil {
		t.Fatalf("MySQL接続失敗: %v", err)
	}
	defer adminDB.Close()
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + tempDBName); err != nil {
		t.Fatalf("一時DBの事前クリーンアップに失敗: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + tempDBName); err != nil {
		t.Fatalf("一時DB作成に失敗: %v", err)
	}
	defer func() {
		if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + tempDBName); err != nil {
			t.Errorf("一時DBの後始末に失敗(手動でDROP DATABASE %sが必要): %v", tempDBName, err)
		}
	}()

	m, err := migrate.New("file://../../migrations", "mysql://"+tempDSN)
	if err != nil {
		t.Fatalf("マイグレーション初期化に失敗: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		t.Fatalf("migrate up(1回目)に失敗: %v", err)
	}

	dataDB, err := sql.Open("mysql", tempDSN)
	if err != nil {
		t.Fatalf("一時DBへの接続に失敗: %v", err)
	}
	defer dataDB.Close()

	// Keycloak連携ユーザーを1人作る(000009でseedされるローカルユーザーとは別に、
	// user_keycloaksを持つ行がdown後も正しくkeycloak_subへ復元されることを見るため)
	if _, err := dataDB.Exec(
		`INSERT INTO users (email, name, role, created_at, updated_at) VALUES (?, ?, 1, NOW(), NOW())`,
		"kc-user@example.com", "KC User",
	); err != nil {
		t.Fatalf("テスト用ユーザー作成に失敗: %v", err)
	}
	if _, err := dataDB.Exec(
		`INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at)
		 SELECT id, ?, NOW(), NOW() FROM users WHERE email = ?`,
		"kc-sub-12345", "kc-user@example.com",
	); err != nil {
		t.Fatalf("テスト用user_keycloaks作成に失敗: %v", err)
	}

	// 000008(split_user_credentials)のdownが適用された状態まで絶対バージョン指定でdownする
	// 【CONTRACT.mdセクション19で判明】以前は m.Steps(-2)(相対ステップ数)を使っていたが、
	// これは「現在の最新マイグレーションが000009である」という前提に依存しており、
	// その後 000010 を追加した際に「2ステップ戻る」の意味が 000009→000008 ではなく 000010→000009 に変わってしまい、テストが壊れた
	//
	// 絶対バージョン指定のMigrate(N)なら将来マイグレーションが増えても意図(000008のdownを適用した状態を再現する)が変わらない
	//
	// golang-migrateの`Migrate(N)`は「バージョンNのupが適用された状態」を指す
	// (=Nのdownは未適用)ため、000008のdownまで適用したい場合はN=7(000007適用済み、000008は未適用)を指定する
	if err := m.Migrate(7); err != nil {
		t.Fatalf("migrate down(バージョン7まで)に失敗: %v", err)
	}

	// 【本題】down後、users.keycloak_subカラムが復元され、かつ元のデータが
	// 正しくコピーバックされていること、UNIQUE制約も復元されていることを確認する
	var keycloakSub sql.NullString
	if err := dataDB.QueryRow(
		`SELECT keycloak_sub FROM users WHERE email = ?`, "kc-user@example.com",
	).Scan(&keycloakSub); err != nil {
		t.Fatalf("down後のusers.keycloak_sub取得に失敗: %v", err)
	}
	if !keycloakSub.Valid || keycloakSub.String != "kc-sub-12345" {
		t.Errorf("down後にkeycloak_subが正しく復元されていません: got=%+v, want=kc-sub-12345", keycloakSub)
	}

	// down後、user_keycloaks/user_passwordsテーブル自体が削除されていること
	for _, table := range []string{"user_keycloaks", "user_passwords"} {
		var count int
		err := dataDB.QueryRow(
			`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?`,
			tempDBName, table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("テーブル存在確認クエリに失敗(%s): %v", table, err)
		}
		if count != 0 {
			t.Errorf("down後も%sテーブルが残っています(DROP TABLEされていない)", table)
		}
	}

	// down後、keycloak_subにUNIQUE制約が復元されていること
	// (以前はここが欠落しており、down後は誰でも重複したkeycloak_subを挿入できてしまっていた)
	var indexCount int
	if err := dataDB.QueryRow(
		`SELECT COUNT(*) FROM information_schema.statistics
		 WHERE table_schema = ? AND table_name = 'users'
		   AND index_name = 'index_users_on_keycloak_sub' AND non_unique = 0`,
		tempDBName,
	).Scan(&indexCount); err != nil {
		t.Fatalf("UNIQUE KEY確認クエリに失敗: %v", err)
	}
	if indexCount == 0 {
		t.Error("down後、users.keycloak_subのUNIQUE KEYが復元されていません")
	}

	// 最後に、down後の状態から再度upできること(往復可能であること)も確認する
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up(2回目、down後の再適用)に失敗: %v", err)
	}
}

func init() {
	// dbNamePatternが想定通りマッチすることをパッケージ初期化時に軽く自己チェックする
	// (正規表現の書き間違いをテスト実行前に検知するための保険)
	if !dbNamePattern.MatchString("root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true") {
		panic(fmt.Sprintf("dbNamePatternが既定DSN形式にマッチしません: %s", dbNamePattern.String()))
	}
}
