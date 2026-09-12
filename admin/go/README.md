# admin-go

Feature Flag(`feature_flags`/`feature_flag_audit_logs`)を管理するための、独立した
Go + Gin製の小さな管理画面
CONTRACT.mdセクション13参照

`admin/rails/`(Ruby on Rails実装)と全く同じCRUD機能を、あえて2つの技術で別々に実装した比較学習用のアプリケーション

## 前提

**この2テーブルの作成マイグレーションはこのアプリには無い**
正本は `training-go/bff-gin/backend/migrations/` のgolang-migrateが管理しているため、
先にbackend側のマイグレーションを実行しておくこと

```sh
cd ../../backend
go run ./cmd/migrate up
```

MySQLはdocker-compose(training-go/bff-gin/docker-compose.yaml)のmysqlサービスが起動済みであること

## 起動

```sh
cd admin/go
go run ./cmd/server
# http://localhost:8091
```

環境変数なしで既定値のまま起動できる

| 環境変数 | 既定値 | 説明 |
|---|---|---|
| `PORT` | `8091` | 待受ポート |
| `DB_DSN` | `root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true` | MySQL接続文字列(docker-compose.yamlのmysqlサービスに対応) |
| `ADMIN_BASIC_AUTH_USER` | `admin` | HTTP Basic Authのユーザー名 |
| `ADMIN_BASIC_AUTH_PASSWORD` | `password` | HTTP Basic Authのパスワード(admin/railsの既定値と統一) |

ブラウザで`http://localhost:8091`を開くとBasic Auth認証を求められる(上記既定値でログイン可能)

## 画面

- `/` — フラグ一覧
- `/flags/:id/edit` — 編集(`enabled`のON/OFF、`default_variation`のon/off切り替え)
  - 保存時に`feature_flag_audit_logs`へ1行追記する
- `/flags/:id/audit_log` — 変更履歴
- `/users` — ユーザー一覧・新規作成・role変更・削除(CONTRACT.mdセクション17)
  - 一覧に「パスキー」列を追加済み(CONTRACT.mdセクション22.7)
  - backendの`GET /internal/v1/admin/users`が返す`has_passkey`をそのまま表示するだけで、admin/go側でパスキーの中身(公開鍵等)を扱うことはない

## 認証について(意図的な簡略化)

このアプリはOIDC(Keycloak)を導入していない
理由はCONTRACT.mdセクション13の通り、
このアプリの主眼は「同じCRUD画面をGoとRailsで実装して比較する」ことであり、
認証方式の比較は既にbackend/bffで十分行っているため、HTTP Basic Authで簡略化している

## テスト

```sh
go test ./...                                   # DB不要な単体テストのみ
TEST_DB_DSN="root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true" go test ./... -v  # 実MySQL込み
```
