// cmd/migrate は golang-migrate を使ったマイグレーション実行コマンド
// Rails対比: `rails db:migrate` / `rails db:rollback` に相当する
//
// 使い方:
//
//	go run ./cmd/migrate up      # DB_DSN未設定時は既定値(docker-compose.yamlのmysqlサービス)を使う
//	go run ./cmd/migrate down
//
// README.mdに記載の手順から呼ばれることを想定している
package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// defaultDBDSN はinternal/config/config.goのDB_DSN既定値と同じ値にする
// (統合検証で判明: 以前はここだけ独自にos.Getenvしており、READMEに書いた
// 「DB_DSN未設定でも既定値で動く」という説明とcmd/migrateの実際の挙動がずれていた)
const defaultDBDSN = "root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true"

func main() {
	if len(os.Args) < 2 {
		log.Fatal("使い方: go run ./cmd/migrate [up|down|version]")
	}
	command := os.Args[1]

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = defaultDBDSN
	}

	// golang-migrateのmysqlドライバは "mysql://" スキーム付きDSNを要求する
	// backend/internal/db側(go-sql-driver/mysql形式、素のDSN)とは書式が違うため、
	// ここで変換する(2つのDSN形式が混在するのはgolang-migrateの仕様上の制約)
	migrationsPath := "file://migrations"
	m, err := migrate.New(migrationsPath, "mysql://"+dsn)
	if err != nil {
		log.Fatalf("マイグレーション初期化に失敗しました: %v", err)
	}
	defer m.Close()

	switch command {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("マイグレーション(up)に失敗しました: %v", err)
		}
		fmt.Println("マイグレーション(up)が完了しました。")
	case "down":
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("マイグレーション(down)に失敗しました: %v", err)
		}
		fmt.Println("マイグレーション(down 1ステップ)が完了しました。")
	case "version":
		version, dirty, err := m.Version()
		if err != nil {
			log.Fatalf("バージョン取得に失敗しました: %v", err)
		}
		fmt.Printf("version=%d dirty=%v\n", version, dirty)
	default:
		log.Fatalf("未知のコマンドです: %s(up/down/versionのいずれかを指定)", command)
	}
}
