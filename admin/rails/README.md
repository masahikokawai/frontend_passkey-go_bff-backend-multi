# admin/rails — Feature Flag管理画面(Rails実装)

`training-go/bff-gin/admin/go/`(Go+Gin実装)と同じ機能をRailsで実装したもの
同じMySQLテーブル(`feature_flags` / `feature_flag_audit_logs`)に対するCRUD画面を2つの技術で実装して比較する学習用アプリケーション(CONTRACT.mdセクション13参照)

## 前提

- Ruby 3.3系、Rails 8.0系
- **先に`training-go/bff-gin/backend`側のマイグレーションを実行しておくこと**
  (`feature_flags`/`feature_flag_audit_logs`テーブルは、このRailsアプリではなく
  backendのgolang-migrateが正本として作成する)

```sh
cd ../../backend
go run ./cmd/migrate up
```

## セットアップ

```sh
cd admin/rails
bundle install
```

`config/database.yml`は、
docker-composeのmysqlサービス(既定`127.0.0.1:13306`、`root`/パスワード無し)上の`bff_gin_development`データベースへ、
development/production ともに接続する(`DB_HOST`/`DB_PORT`/`DB_USERNAME`/`DB_PASSWORD`/`DB_NAME`で上書き可能)

このアプリ自身はfeature_flagsテーブルを作成しない

## 起動

```sh
bundle exec rails server -p 8092
# http://localhost:8092 (既定ポート8092。PORT環境変数で上書き可)
```

HTTP Basic Authで保護されている
既定の資格情報(学習用のダミー値):
```
ADMIN_BASIC_AUTH_USER=admin
ADMIN_BASIC_AUTH_PASSWORD=password
```

環境変数で変更できる

## 画面

- `/` — フラグ一覧(flag_key・説明・enabled・default_variation・更新日時)
- `/feature_flags/:id/edit` — enabled(有効/無効)・default_variation(on/off)を編集
- `/feature_flags/:id/audit_logs` — そのフラグの変更履歴(誰が・いつ・何を変更したか)

更新のたびに`feature_flag_audit_logs`へ1行追記され、`changed_by`にはBasic Auth のユーザー名が入る

- `/users` — ユーザー一覧・新規作成・role変更・削除(CONTRACT.mdセクション17、`BackendUsersClient`経由でbackendへ委譲)
  - 一覧に「パスキー」列を追加済み(CONTRACT.mdセクション22.7)
  - backendの`GET /internal/v1/admin/users`が返す`has_passkey`をそのまま表示するだけで、admin/rails側でパスキーの中身(公開鍵等)を扱うことはない
  - backendがこのフィールドをまだ返さない場合でも`nil`(falsy)として扱われ、「未登録」表示になるだけでエラーにはならない

## テスト

```sh
# 初回のみ
## 専用のadmin_rails_testを作成
RAILS_ENV=test bundle exec rails db:create
bundle exec rspec
```

`spec/support/schema.rb`が、テスト専用DB(`admin_rails_test`。共有DBとは別)に対してのみ
`feature_flags`/`feature_flag_audit_logs`と同じスキーマを生SQLで再現してからテストを
実行する(このアプリの通常のマイグレーションでは作らないテーブルのため)

`spec/models/`(モデル、5件)・`spec/requests/`(コントローラ、7件)に加え、
`spec/system/feature_flags_spec.rb`(1件)ではCapybara(rack_testドライバ、実ブラウザ不要)で
「一覧→編集→保存→一覧反映→変更履歴確認」という画面遷移込みの簡易的なe2eを1本用意している

## admin/go(Go+Gin実装)との比較ポイント

| 観点 | Rails | Go+Gin |
|---|---|---|
| スキーマ管理 | 自前のマイグレーションを持たず、backendの成果物に相乗り | 同左 |
| 認証 | `http_basic_authenticate_with`(ApplicationController) | `gin.BasicAuth()`ミドルウェア |
| ORM | ActiveRecord(`self.table_name`で既存テーブルにマッピング) | GORM |
| ビュー | ERB + Bootstrap 4.3.1(CDN) | html/template + Bootstrap 4.3.1(CDN) |
| テスト | RSpec(モデル・リクエストスペック) | 標準`testing`パッケージ、`google/go-cmp` |
