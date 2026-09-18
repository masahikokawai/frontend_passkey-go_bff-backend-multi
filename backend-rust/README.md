# backend-rust(Rust + Axum + tonic)

CONTRACT.mdセクション20(backend多言語実装比較)の一環として、`backend/`(Go実装)と
**同一のワイヤー契約**(REST v1のJSON形状・ステータスコード・エラーボディ、gRPC v2の
`.proto`・ステータスコード)を持つTask CRUDをRustで実装したもの。既存の`backend/`には
一切手を加えていない。対象範囲はTask CRUDのみ(Label/User管理/Feature Flag管理APIは
セクション20.1により対象外、引き続きGoのみ)。

## なぜAxum + tonicか

CONTRACT.mdセクション20.4で選定した理由の通り、tokio/hyper/tonicと同じ開発元(Tokioチーム)が
Axumも開発しており、どちらも`tower`のServiceトレイトを共有基盤にしている。そのため
REST(Axum)とgRPC(tonic)を同一プロセス・同一言語で共存させる今回のようなケースで、
最も自然に組み合わせられる。

## セットアップ

```sh
cd backend-rust
# Rust未インストールの場合
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
cargo build
```

DBへ接続していない状態でも`cargo build`が通ることを確認済み(`sqlx::query`/`query_as`の
実行時バインド版を使っており、`query!`マクロ系のようなコンパイル時DB接続を要求しない設計)。

## 実行

```sh
# frontend_passkey-go_bff-backend-multi/ ルートで
docker compose up -d --wait mysql keycloak

cd backend-rust
cargo run
```

既定でREST(Axum)は`:8093`、gRPC(tonic)は`:9093`で待ち受ける(Go実装の`:8090`/`:9090`と
衝突しない値)。環境変数はGo実装(`backend/internal/config/config.go`)の命名規則・既定値を
そのまま踏襲している(`DB_DSN`のみURL形式が必要なため値の書式が異なる。下記「Go実装との
既知の差異」参照)。

| 環境変数 | 既定値 | 備考 |
|---|---|---|
| `HTTP_ADDR` | `:8093` | Go実装の`:8090`とは別ポート |
| `GRPC_ADDR` | `:9093` | Go実装の`:9090`とは別ポート |
| `DB_DSN` | `mysql://root@127.0.0.1:13306/bff_gin_development` | sqlxはURL形式のDSNが必要 |
| `KEYCLOAK_ISSUER` | `http://localhost:8082/realms/training` | Go実装と同じ |
| `EXPECTED_AUDIENCE` | `backend` | Go実装と同じ |
| `LOCAL_AUTH_HMAC_SECRET` | `local-dev-hmac-shared-secret-change-me` | Go実装と同じ |
| `LOCAL_AUTH_RSA_JWKS_URL` | `http://localhost:8080/.well-known/jwks.json` | bffの公開鍵(Go実装と同じ) |
| `LOG_LEVEL` | `info` | `RUST_LOG`相当を兼ねる(tracing-subscriber) |

## 動作確認

- `cargo build` / `cargo build --tests`: 通ることを確認済み
- `cargo test`: 単体テスト38件、すべてpass(DB接続不要、後述)
- `cargo test -- --ignored --test-threads=1`: DBに実接続する結合テスト10件、docker-compose上のMySQLに
  対して実際にすべてpassすることを確認済み(後述)。`--test-threads=1`を付けずデフォルトの並列実行にすると、
  各テストが個別に`db::connect`でコネクションプールを開くため、DB混雑により無関係な理由で
  一時的に失敗することがある(既知の環境要因、テストロジック自体の欠陥ではない)。確認時は
  `--test-threads=1`を付けて直列実行することを推奨する
- `cargo run`でREST(:8093)・gRPC(:9093)を実際に起動し、ローカルHMAC発行の実JWTを使って
  `curl`でTask一覧取得・作成・削除がGo実装と同じJSON形状で動くことを確認済み
- `cargo run --example grpc_smoke`で、同じくgRPC側のListTasks/CreateTask/DeleteTaskが
  実際に動くことを確認済み(`TOKEN=<bearer token> cargo run --example grpc_smoke`)

## 外部公開API(CONTRACT.mdセクション11・20、追加実装)

bffを経由しない外部公開API(`/external/v1/tasks`)を、内部CRUDと同じプロセス内の3つ目の
リスナーとして追加した。

- ポート: `:8098`(`EXTERNAL_HTTP_ADDR`環境変数、既定値)。内部REST(`:8093`)・
  内部gRPC(`:9093`)・Go実装の内部アドレス(`:8097`)・gateway(`:8081`)のいずれとも別
- 認証: Keycloak発行のClient Credentials Grantトークンのみ受け付ける(ローカルHMAC/RSAは
  対象外)。JWKS検証に加えて`azp`クレームが`EXTERNAL_API_CLIENT_ID`(既定`external-api-client`)と
  一致することを確認する(backend(Go)の`RequireExternalClientAuth`と同じ2段チェック)
- ページネーション: `backend.external-tasks-pagination-v2`というFeature Flagで
  offset(v1)/cursor(v2)を切り替える。**このflagはGo/Rust/Scala(http4s)/Scala(Pekko)/Railsの
  5言語で共有する1つのflag**(CONTRACT.mdセクション20、ユーザーの明示的な指示)。
  backendは自分のMySQL接続で`feature_flags`テーブルを直接ポーリングする(bff/gatewayのような
  HTTPポーリングではなく、Go実装の`internal/featureflag/mysql_retriever.go`と同じ「DBを正本として
  直接読む」設計。10秒間隔でキャッシュ更新)
- レスポンス形状は`backend/internal/handler/external/task.go`と完全に一致させている
  (`user_id`をレスポンスに含めない点も含む。内部CRUDのレスポンスとは異なるので注意)
- cursorの実体はGoと同じ`base64url(padding付き)("<RFC3339Nano>|<id>")`という不透明文字列
  (`src/external/cursor.rs`)
- 実機確認: Keycloakから`external-api-client`のClient Credentials Grantで実トークンを取得し、
  v1(offset, 既定)・v2(cursor、flagをONにして確認)双方でTask一覧取得が期待通りのJSON形状で
  返ることを確認済み。トークン無し(401 `unauthorized`)・不正トークン(401 `invalid_token`)・
  `user_id`欠落(400 `user_id is required`)もGo実装と同じエラー形状で返ることを確認済み

## テスト構成

- `src/model.rs`・`src/error.rs`・`src/auth/jwt.rs`・`src/rest/task.rs`・`src/flags.rs`・
  `src/external/cursor.rs`内の`#[cfg(test)]`:
  バリデーション(name必須・20文字超え・過去日禁止・status不正)、RESTエラー→ステータス
  コード/JSONボディのマッピング(401/400/403/404/422/500全パターン、外部公開API専用の
  `invalid_token`/`client_not_allowed`/`user_id is required`/`invalid user_id`含む)、
  HMAC JWTの検証・Dispatcherのissuer振り分け、JSON整形(`task_to_json`)、
  `label_ids`のパース、flag variationsの解決ロジック、外部cursorのencode/decode往復など、
  DB/ネットワーク接続なしで完結する純粋なロジックを網羅している(38件)
- `tests/integration_test.rs`: docker-compose上のMySQLに実接続する結合テスト。
  backend(Go)の`backend/test/integration`が`-tags=integration`で通常のテストから
  分離されているのと同じ考え方で、既定の`cargo test`では実行されない`#[ignore]`にしてある
  (`cargo test -- --ignored`で実行)。`resolveUserID`のローカル発行issuer/Keycloak発行issuer
  それぞれでのユーザー解決・未プロビジョニング判定、REST v1のCreate→List→Get→Update→Delete
  一連のフロー、401/403/400/422/404の実際のステータスコードを確認する。テストごとに
  一意なメール/keycloak_subでユーザー行を作り、アサーション失敗(panic)時も
  `tokio::spawn`+`JoinError`で受け止めて必ず後片付け(cleanup)が走るようにしている
  (共有の開発用DBを汚さないため)
- `tests/external_integration_test.rs`: 同じく`#[ignore]`。`backend.external-tasks-pagination-v2`の
  enabled/default_variationを直接書き換えて`fetch_bool`が追従することの確認(テスト後に
  必ず元の値へ復元する)、offsetページングのソート順(`created_at DESC, id DESC`)・total件数、
  cursorページングが重複無く次ページへ続くことを確認する(3件)

## Go実装との突き合わせで見つかった実装ミス(このRust実装自体のバグ、修正済み)

- **`users`テーブルは`keycloak_sub`カラムを持たない**: 当初`users.keycloak_sub`を直接
  検索する実装にしていたが、実際は`migration 000008_split_user_credentials`で
  `user_keycloaks`テーブル(`user_id`/`keycloak_sub`)へ分離済みだった。backend(Go)の
  `repository.User.GetByKeycloakSub`が`user_keycloaks`をJOINしているのに合わせ、
  `db::find_user_by_keycloak_sub`もJOINするよう修正した。CONTRACT.mdセクション5.1の
  例だけを見て実装すると踏む典型的な罠で、実際に結合テストの初回実行で
  `Unknown column 'keycloak_sub' in 'field list'`として発覚した

## Go実装との既知の差異

- **DSNの書式**: backend(Go)の既定値`root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true`
  はGoの`go-sql-driver/mysql`固有の書式。sqlxはURL形式(`mysql://root@127.0.0.1:13306/...`)を
  要求するため、接続先(host/port/user/db)は同一だが文字列表現は異なる。ワイヤー契約
  (クライアントに見える挙動)には影響しない内部実装の違い
- **REST v1のレスポンスに`user_id`を含めない**: CONTRACT.mdセクション5.1の記載例には
  `user_id`が書かれているが、backend(Go)の実装(`taskDTOToJSON`)は実際には含めていない
  (`service.TaskDTO`自体に`UserID`フィールドが無い)。本実装はドキュメントではなく
  **実際のGo実装の挙動**に合わせている(セクション20.5のワイヤー契約パリティの原則)。
  bff側の`taskV1DTO`は`user_id`用フィールドを持つが無くてもゼロ値のまま実害はない
- **N+1の意図的再現はしていない**: backend(Go)のv1(REST)はCONTRACT.mdセクション5の学習教材として
  意図的にN+1クエリを再現しているが、これはGoのStrangler Fig学習デモそのものであり、
  セクション20(言語比較)の主題ではないため、Rust実装は素直に効率的な実装(ラベルの一括JOIN)にしている
- **JWTの`nbf`検証は簡略化している**: backend(Go、golang-jwt/jwt v5)は`RegisteredClaims`が
  実装する`Validator`インターフェース経由で、`nbf`クレームが存在すれば常に検証する。
  Rust側(`jsonwebtoken`)も`nbf`をClaimsに含めているが、トークンにこのクレームが
  無い場合の挙動の細部(必須扱いにするかどうか)まではGo実装と完全に一致させていない。
  Task CRUDの主要な挙動(認可・データ整合性)には影響しない部分と判断し、時間の都合で
  「実務ならここも詰めるべき既知の簡略化」として明記するに留めた

## Axum+tonic vs Gin+gRPC-Go 実装してみての所感

- **1バイナリでREST+gRPCを両立させる設計は驚くほど素直だった**: `tokio::spawn`で
  Axumのサーバーとtonicのサーバーをそれぞれ別タスクとして立ち上げ、`AppState`
  (DBプール+JWT Dispatcher)を`Arc`で共有するだけで済む。Goのcmd/server/main.goが
  goroutineで同じことをしているのと発想は同じだが、Rustの所有権チェックのおかげで
  「どちらのタスクも同じ`Arc<AppState>`を安全に共有している」ことがコンパイル時に
  保証される点は書いていて安心感があった
- **`sqlx`のコンパイル時チェックを諦めた判断は正しかった**: `sqlx::query!`マクロは
  ビルド時にDBへ接続してSQLの型を検証してくれる強力な機能だが、それを使うと
  「DB無しでは`cargo build`すら通らない」プロジェクトになってしまう。学習用の
  比較実装として「まずビルドが通る」体験を優先し、実行時バインドの`sqlx::query`に
  留めた。その代わりカラム名のtypoやJOIN先テーブルの取り違え(実際に`keycloak_sub`の
  件で踏んだ)はコンパイラが教えてくれず、実行して初めて気づく形になった。
  Goのgormも似た性質(文字列ベースのカラム指定)を持つため、この点でのトレードオフは近い
- **`jsonwebtoken`クレートのJWKS対応の楽さ**: Goの`jwks.go`は、JWKのn/e(base64url、
  大きな整数)から`rsa.PublicKey`を自分で組み立てるコード(`math/big`を使った手組み)が
  必要だったが、Rustの`jsonwebtoken`は`DecodingKey::from_rsa_components(n, e)`が
  base64url文字列を直接受け取ってくれるため、その部分の実装量はGoよりかなり少なく済んだ
- **エラー型の分岐をenumで表現できる心地よさ**: `RestError`をenumにして
  `IntoResponse`を1箇所実装するだけで、ハンドラ側は`?`演算子で早期リターンするだけの
  素直なコードになった。Goの`renderServiceError`(switch文で`errors.Is`を繰り返す)と
  発想は同じだが、Rustのenumは「取りうるエラーの全パターン」がコンパイラに把握されている
  分、ハンドラ追加時に分岐の書き漏れに気づきやすい
- **gRPC側のエラーは`tonic::Status`のヘルパー関数(`Status::not_found`等)で
  Goの`status.Error(codes.NotFound, ...)`とほぼ1対1に書けた**。gRPCのステータスコード
  体系自体が言語非依存の規約であるため、ここは移植というより「同じ概念を別の言語の
  API経由で呼ぶだけ」という感覚に近かった
- **`tokio::spawn`+`JoinError`によるテストの後片付け保証は、Rustならではの発見だった**:
  Goなら`defer cleanup()`で素直に書ける「アサーション失敗時も後片付けする」処理が、
  Rustの`async fn`テストでは`assert_eq!`のpanicがteardownコードをスキップしてしまう
  (`catch_unwind`はFutureに直接使えない)。`tokio::spawn`したタスクの`JoinError`で
  panicかどうかを判定し、外側で確実にcleanupしてから`resume_unwind`する、という
  少し回りくどいパターンが必要だった。実際にこれをやっていなかった初期実装では、
  結合テストの試行錯誤中に共有の開発用DBへテスト用ユーザー行が3件残ってしまう事故を
  起こしており(手動で削除して復旧)、学習効果としては良い教訓になった
