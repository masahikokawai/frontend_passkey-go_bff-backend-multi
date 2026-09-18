# backend-rails(Ruby on Rails, ActionController + grpc gem)

CONTRACT.mdセクション20: backendのTask CRUD実処理を、既存Go実装(`backend/`)と全く同じ
ワイヤー契約(REST JSON形状・ステータスコード・エラーボディ、gRPCの`.proto`・ステータスコード)で
Railsに移植したもの。**既存の`admin/rails`(Feature Flag/ユーザー管理のadmin画面)とは別物**で、
こちらはbackendの役割(Task CRUD本体)を担う新しいコンポーネント。

`bff`は`backend.task-language`フラグの値に応じて、このコンポーネント(値`rails`)へ
ルーティングできる(bff側の配線は別ステップで対応)。`tasks`/`labels`/`task_labels`/`users`の
4テーブルは`backend`(Go)のgolang-migrateが正本として管理しており、このアプリ自身は
これらのテーブルを作成するマイグレーションを持たない(development/productionは既存の
`bff_gin_development`データベースへそのまま接続する。config/database.yml参照)。

## セットアップ

```sh
cd backend-rails
bundle install
```

gRPCのRubyスタブは`.proto`から事前生成済み(`lib/gen/task/v1/`)。`.proto`を変更した場合は
以下で再生成する。

```sh
bundle exec grpc_tools_ruby_protoc --ruby_out=lib/gen --grpc_out=lib/gen -I proto proto/task/v1/task.proto
```

## 実行

**Railsのプロセスモデル上の理由から、REST(Puma)とgRPC(GRPC::RpcServer)を2つの別プロセスとして
起動する**(後述「Go実装との比較所感」参照)。

```sh
# frontend_passkey-go_bff-backend-multi/ ルートで
docker compose up -d --wait mysql redis keycloak swagger-ui
cd backend && go run ./cmd/migrate up  # tasks/labels/task_labels/usersのスキーマはこちらが正本

cd backend-rails
bundle exec puma -C config/puma.rb      # REST v1相当: :8096
bundle exec bin/grpc_server             # gRPC v2相当: :9096(別プロセス、別ターミナル)
```

環境変数(既定値はGo実装・docker-compose.yamlに合わせてある):

| 変数 | 既定値 |
|---|---|
| `HTTP_ADDR` | `:8096` |
| `GRPC_ADDR` | `:9096` |
| `DB_HOST`/`DB_PORT`/`DB_NAME`/`DB_USERNAME`/`DB_PASSWORD` | `127.0.0.1`/`13306`/`bff_gin_development`/`root`/(空) |
| `KEYCLOAK_ISSUER` | `http://localhost:8082/realms/training` |
| `EXPECTED_AUDIENCE` | `backend` |
| `LOCAL_AUTH_HMAC_SECRET` | `local-dev-hmac-shared-secret-change-me` |
| `LOCAL_AUTH_RSA_JWKS_URL` | `http://localhost:8080/.well-known/jwks.json`(bffのJWKS) |

## テスト

```sh
# 初回のみ: 専用テストDBを作成(backend_rails_test、共有DBとは別。schema.rbが自動でテーブルを作る)
docker compose exec mysql mysql -uroot -e "CREATE DATABASE IF NOT EXISTS backend_rails_test;"

RAILS_ENV=test bundle exec rspec
```

`spec/models/task_record_spec.rb`(バリデーション・JSON契約)・`spec/services/jwt_verifier_spec.rb`
(3issuer振り分け、HMAC/RSA JWKS検証)・`spec/services/user_resolver_spec.rb`(resolveUserID相当)・
`spec/requests/tasks_spec.rb`(REST CRUD全体・エラーステータスコード・認可)を実装(32件、全pass確認済み)。

## 動作確認(実機)

docker-compose上のMySQL(`bff_gin_development`、既存の`local-user@example.com`)へ接続し、
ローカルHMAC方式相当のJWTを自前で発行してREST(:8096)・gRPC(:9096)双方に対して
List/Get/Create(labelあり)/Update/Delete、および主要エラーケース(未認証401・不正トークン401・
存在しないid 404・status不正422・name必須違反400・name長すぎ422 validation_error・
未プロビジョニング403)を実際に実行し確認した。テスト後に投入したデータは削除済み。

## 外部公開API(CONTRACT.mdセクション11、BFF非経由)

内部CRUDとは別に、Client Credentials Grant認証の外部公開API(`GET /external/v1/tasks`)も実装している。

- **別プロセス**: 内部REST(:8096)・gRPC(:9096)に加えて、外部公開API専用のPumaプロセスを
  `config/puma_external.rb`(既定`:8101`)で起動する(Railsは合計3プロセス構成になる)
  ```sh
  bundle exec puma -C config/puma_external.rb   # 外部公開API: :8101(3つ目の別プロセス)
  ```
- **認証**: `JwtVerifier`(内部CRUDと共用)でKeycloak JWKS(RS256)検証した上で、`azp`クレームが
  `EXTERNAL_API_CLIENT_ID`(既定`external-api-client`)と一致するかを追加チェックする
  (`app/controllers/external/v1/tasks_controller.rb`)
- **ページング**: `backend.external-tasks-pagination-v2`というflag(**5言語で共有する1つのflag**、
  ユーザーの明示的な指示による設計)のON/OFFで、offsetページング(`page`/`page_size`/`total`)と
  keyset cursorページング(`cursor`/`limit`/`next_cursor`)を切り替える。このRailsアプリ自身が
  `feature_flags`テーブルを直接SELECTして評価する(Go実装の`featureflag.NewMySQLEvaluator`と同じ設計、
  `ExternalPaginationFlag`、10秒キャッシュ)。bffやgatewayのようなHTTPポーリングは使わない
- レスポンスのTask JSON形状は内部CRUDと同じだが、**`user_id`フィールドは含めない**
  (Go実装の`taskDTOToJSON`の実際の挙動に合わせた、`as_external_contract_json`)
- cursorは`base64url("<RFC3339Nano相当>|<id>")`という不透明文字列(`ExternalCursor`)。
  Rubyの`Time#iso8601(9)`は常に9桁のナノ秒を出力する(Goの`RFC3339Nano`は末尾0を切り詰める)ため
  cursor文字列そのもののバイト列はGo実装とは一致しないが、どちらの形式も相互に`decode`可能
  (ISO8601のパースは桁数に寛容なため)。cursorは「自分が発行したものを自分が読み戻す」opaqueな
  値であることが契約上の要件であり、言語間でのバイト単位一致は求められていない
- テスト: `spec/services/external_pagination_flag_spec.rb`(flag評価・キャッシュ)・
  `spec/services/external_cursor_spec.rb`(cursorのencode/decode往復)・
  `spec/requests/external_tasks_spec.rb`(認証・バリデーション・offset/cursor両ページング)を追加(17件)
- 実機確認: Keycloakから`external-api-client`のClient Credentials Grantで実トークンを取得し、
  `:8101`に対してoffset/cursor両モードでのタスク取得、認証エラー(トークン無し・不正トークン・
  azp不一致)、flag切り替え後の反映を確認済み

## Go実装との比較所感

- **REST/gRPCを1プロセスに同居させにくい**: Go実装は`cmd/server/main.go`で1つのバイナリの中に
  goroutineでGin(REST)とgRPCサーバを両方起動し、1プロセス2ポートという構成が非常に自然だった。
  RailsではPuma(マルチスレッドWebサーバー)と`GRPC::RpcServer`(ブロッキングのイベントループを
  内部に持つ)を同一プロセスで共存させるのは一般的な構成ではなく、素直に実装しようとすると
  Rubyのスレッド/GIL周りの相性を気にする必要が出てくる。今回は素直に2プロセス
  (`puma`と`bin/grpc_server`)に分けたが、「1バイナリに複数のサーバーを同居させる」という
  Goでは当たり前の発想が、Railsのエコシステムでは自然にはできない、という違いが実装して
  初めて実感できた。
- **ActiveRecordの`has_many :through`は未保存レコードへの`label_ids=`に弱い**: `Task.new(...)`
  (未保存)に対して`label_ids = [...]`してから`save`すると、中間テーブル`TaskLabel`の
  `belongs_to :task`(Rails 5+の既定`required: true`)の検証タイミングの都合で
  「Task labels is invalid」という分かりにくいエラーになる(実機検証で発覚)。
  Go/Rust/Scala実装には無い、Railsならではの落とし穴で、回避策として「まずtask本体を保存し、
  永続化された後でlabelsを紐付け直す」という2段階の書き方にした
  (`app/controllers/internal/v1/tasks_controller.rb`のcreateコメント参照)。
- **`Task`という名前がそのまま使えない**: `protoc-gen-ruby`はprotoの`package task.v1`から
  Rubyの名前空間を`Task::V1::...`として生成する固定の命名規則を持っており(`.proto`側で
  回避するオプションは無い)、ActiveRecordモデルを素直に`class Task`にすると生成コードの
  トップレベル定数`Task`と衝突して起動時エラーになる(`TypeError: Task is not a module`)。
  そのためモデル名を`TaskRecord`にし、`self.table_name = "tasks"`で対応した。Go/Rust/Scala側は
  proto生成コードが独立した名前空間(パッケージ)に入るためこの種の衝突は起きず、
  Ruby(というよりgoogle-protobufのRuby実装)特有の制約だった。
- **ActiveRecordのバリデーション・enum・includesは表現力が高く簡潔**: `validates :name,
  length: { maximum: 20 }`や`enum :status, {...}`、N+1回避の`includes(:labels)`は、
  Go実装が自前のスキャナ・バリデーション関数・repository層で書いていた内容と比べて
  圧倒的に記述量が少ない。一方で、上記のような「暗黙の規約に依存した部分でハマると原因が
  追いにくい」という副作用(動的型付け・メタプログラミングの裏返し)も、今回のREST/gRPC
  同居やhas_many :throughの件で実感した。
- **JWT検証はほぼ1対1で移植できた**: `backend/internal/authjwt`のDispatcher/Verifier/
  HMACVerifierという設計(issで振り分け、実際の信頼は各verifierの署名検証に委ねる)は、
  `ruby-jwt`の`jwks:`オプション(kid不一致時のみ再取得するcallback)でほぼそのままRubyに
  移植できた。この部分の設計自体は言語非依存で移植性が高いことが分かった。
