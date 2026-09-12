# backend

`training-go/bff-gin/CONTRACT.md` の実装
private network限定、BFF経由のみ公開

## 初回セットアップで必要な手順

以下は統合時に実行・確認済み(`go build ./...` / `go vet ./...` / `go test ./...` すべて成功)
クリーンな環境でcloneし直した場合は改めて実行すること

1. `go mod tidy` — go.sumの生成、依存解決の検証
2. protoc-gen-go / protoc-gen-go-grpc のインストール:
   ```
   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
   ```
3. コード生成 — `proto/task/v1/task.proto` から `gen/task/v1/` に生成コードを作る
   (`internal/grpcserver/task_service.go` はこの生成物が無いとビルドできない)
   `buf`が使える環境なら `buf generate`
   protocを直接使う場合:
   ```
   protoc --proto_path=proto \
     --go_out=gen --go_opt=paths=source_relative \
     --go-grpc_out=gen --go-grpc_opt=paths=source_relative \
     proto/task/v1/task.proto
   ```
4. `go build ./...` / `go vet ./...` / `golangci-lint run` で最終確認

## 3つのリスナー

backendは同一プロセス内で3つのサーバーを起動する

| 用途 | 既定アドレス | アクセス元 |
|---|---|---|
| REST v1(N+1あり、旧実装) | `:8080` | BFFのみ(private network) |
| gRPC v2(Preload最適化、新実装) | `:9090` | BFFのみ(private network) |
| 外部公開API(Client Credentials) | `:8081` | BFFを経由しない外部クライアント。`127.0.0.1`限定でホスト公開 |

## 外部公開API(BFF非経由)

CONTRACT.mdセクション11参照
`GET /external/v1/tasks?user_id=` を、OAuth2 Client
Credentials Grantで取得したトークン(Keycloakクライアント `external-api-client`)で呼び出す

- Feature Flag `backend.external-tasks-pagination-v2`(`internal/featureflag/flags.yaml`)
  - `"on"` に書き換えると、offsetページング(`page`/`page_size`)からkeyset・cursorページング
  - (`cursor`/`limit`)に切り替わる(反映は最大60秒、GO Feature Flagのポーリング間隔による)
- Feature Flag `backend.external-tasks-orm`(`gorm`/`bob`、既定`gorm`)
  - Task一覧取得の内部実装をGORM/bob([bob](https://github.com/stephenafamo/bob)、
    CONTRACT.mdセクション24)のどちらで動かすかを切り替える
  - `backend.external-tasks-pagination-v2`とは独立した軸なので、4通りの組み合わせ全てが
    到達可能。レスポンスのJSON形状はどちらでも完全に同一(下の「ORM比較」節参照)
- Swagger UIで仕様を確認できる(`docker-compose.yaml`の`swagger-ui`サービス、`backend/openapi/external-api.yaml`を参照)
- 既知の制約: `user_id`はクライアント側が任意に指定できる
  - このAPIの資格情報を持つ者は任意ユーザーのタスクを読み取れる(サーバー間の信頼関係を前提にした設計であり、エンドユーザー単位の認可は行わない)

## パスキー(WebAuthn)関連API(CONTRACT.mdセクション22)

既存ユーザー(bffのローカル認証(HMAC/RSA)ユーザーのみが対象、Keycloak発行ユーザーは対象外)への追加認証手段
go-webauthnライブラリ自体はbff側が使うため、backendは検証済みの値をそのまま
保存/参照するだけの層(`webauthn_credentials`テーブル、`internal/service/webauthn.go`)

| エンドポイント | 認証 | 用途 |
|---|---|---|
| `POST /internal/v1/auth/webauthn/credentials` | 通常のJWT(`RequireAuth`) | ログイン中ユーザーが新しいパスキーを登録。ボディ: `{"credential_id":"base64url","public_key":"base64","sign_count":0,"transports":["internal"],"name":"..."}` → `201 {"id":1}` |
| `GET /internal/v1/auth/webauthn/credentials/:credential_id` | 共有シークレット(`X-Webauthn-Internal-Token`、`WEBAUTHN_INTERNAL_TOKEN`環境変数) | ログイン試行中(未認証)にbffがcredential_idからuser_id・公開鍵・sign_countを引く。見つからなければ404 |
| `PATCH /internal/v1/auth/webauthn/credentials/:credential_id/sign-count` | 同上 | ログイン成功後、リプレイ攻撃対策のsign_countを更新 |

`GET /internal/v1/admin/users`(Admin::Usersの一覧)のレスポンスにも、各ユーザーに
`has_passkey`(bool)が追加されている(`webauthn_credentials`へのuser_id存在チェック)

## ORM比較: GORM / bob(CONTRACT.mdセクション24)

`/external/v1/tasks`(外部公開API)のTask一覧取得ロジックに限定して、
[bob](https://github.com/stephenafamo/bob)によるコード生成ベースの並行実装を追加した
(`internal/repository/task_bob.go`)。既存のGORM実装(`internal/repository/task.go`の
`ListOffsetForExternalAPI`・`ListCursorForExternalAPI`)には一切手を入れておらず、
Feature Flag `backend.external-tasks-orm`(`gorm`/`bob`、既定`gorm`)で切り替える
(`backend.external-tasks-pagination-v2`と直交する2軸目。掛け合わせで
offset×gorm/offset×bob/cursor×gorm/cursor×bob の4パターン全てに到達できる)

### コード生成のワークフロー

```
go install github.com/stephenafamo/bob/gen/bobgen-mysql@v0.50.0
# docker-composeでMySQLを起動した状態で(backend/ で実行):
bobgen-mysql -c ./bobgen.yaml
```

`backend/bobgen.yaml` が設定ファイル。対象を`tasks`/`labels`/`task_labels`の3テーブルに
絞り込み(`mysql.only`)、生成先を`internal/repository/bobgen/`配下にまとめている
(`users`・`feature_flags`等、このタスクの対象外のテーブルはGORMのまま)

生成物(`internal/repository/bobgen/**/*.bob.go`)は「その場で再生成・削除して良い」ファイル
(生成コード先頭のコメント通り)なので、リポジトリにコミットする前提でも
`bobgen.yaml`さえあれば誰でも再生成できる

### 実装してみての所感(GORMとの書き味の違い)

- **型安全性**: bobは列ごとに型付きのGoコード(`Tasks.Columns.UserID.EQ(...)`)を生成するため、
  列名のタイポや型の取り違えがコンパイル時に検出できる。反面、GORMの
  `Where("user_id = ?", v)` や `Order("created_at DESC, id DESC")` のような
  「SQL文字列をそのまま書ける」手軽さは無く、複合ORDER BYも列ごとに
  `sm.OrderBy(col).Desc()` を積み重ねる必要があり、GORMよりコード量は増える
- **Preload相当が無い(最大のハマりどころ)**: GORMは構造体タグ(`many2many:task_labels`)
  さえあれば`Preload("Labels")`でJOINを解決してくれるが、bobのリレーション検出は
  DBのFK制約が前提。`task_labels`にはFK制約が意図的に無い(`migrations/000004`参照、
  以前のテスト監査で発覚した孤立行問題の経緯によるもの)ため、bobgen-mysqlは
  Task↔Labelの間に一切のリレーションヘルパーを生成しなかった。そのため
  `attachLabelsBob`(`internal/repository/task_bob.go`)でGORMのPreloadが内部で
  やっていることと同じ「まとめてIN取得」を手動実装している
- **型システム**: bobは`*string`の代わりに`null.Val[string]`(aarondl/opt)、
  `BIGINT UNSIGNED`の代わりに`types.Uint64`を生成する。GORMの素朴な`*string`/`uint64`とは
  変換が必要になるが、`task_bob.go`の中だけに閉じ込めており、repository層より上
  (service/handler)からはbob固有の型は一切見えない(戻り値は既存の`model.Task`のまま)
- **コネクション**: bobは`database/sql`の`*sql.DB`をそのままExecutorとして使う設計で、
  新しいプールを別途持つ必要が無い。GORMの`*gorm.DB`が内部に持つ`*sql.DB`
  (`db.DB()`)をそのまま共有しているため、接続設定(DSN・タイムゾーン等)は
  GORM側と完全に同じになり、突き合わせテストで時刻のズレ等に悩まされずに済んだ
- **総評**: 学習目的の小さな一覧取得ロジックであればbobの型安全性の恩恵はわずかで、
  GORMの簡潔さのほうが書きやすいと感じた。一方でPreloadに頼れない(FK制約が無い
  スキーマの)場合、GORMも結局手動でJOIN/IN取得を書くことになるため、その場合は
  型付きクエリビルダであるbobの方が「素のSQL文字列を書くよりは安全」という利点が
  はっきり出る、という使い分けの感触を得た

### テスト

- `test/integration/task_external_orm_compare_test.go`: GORM実装・bob実装が同じデータに
  対して完全に同じ結果を返すことをgo-cmpで突き合わせる(offset/cursor両方、repository層・
  service層とも)。あわせてbob実装単体でのページング境界・ラベル同時取得も直接検証している
- `test/integration/external_task_handler_test.go`: `backend.external-tasks-pagination-v2`×
  `backend.external-tasks-orm`の4通りの組み合わせがハンドラ層で正しく切り替わり、
  同じJSON形状を返すことを確認する

## 既知の制約・TODO

- v2(gRPC)のcursorページングは `id` 昇順固定
  - `finished_on`ソート等との併用は未対応(学習目的の単純化)
- Admin::UsersのRole変更はアプリDBの`role`列のみ更新し、Keycloak側realm roleとの逆方向同期は行わない
- UserCredential/UserToken(Railsの招待・パスワード設定フロー)は移植していない
  - Keycloak 自体のユーザー管理・Required Actions に置き換わる前提(CONTRACT.mdセクション10)
- `test/integration/task_v1_v2_compare_test.go` は `TEST_DB_DSN` 環境変数が無い場合スキップされる
  - `go test -tags=integration ./test/integration/...` で実行する
- go.sumは未生成(上記手順1を参照)
