# backend-java

bff-gin: Task CRUD backendのJava実装(CONTRACT.mdセクション20の多言語backend比較の10言語目)

**このフェーズのスコープ**: 内部REST v1・内部gRPC v2のCRUD・JWT/JWKS認証(ローカルHMAC/ローカルRSA/Keycloakの3issuer)・外部公開API・Feature Flagポーリング。bff/gateway/migrationへの配線は別途対応する(下記「未実装・今後の予定」参照)。

## ポート

- 内部REST v1: `:8111`(環境変数`HTTP_ADDR`。他言語と同じくポート番号のみを受け付ける簡略形式)
- 内部gRPC v2: `:9101`(環境変数`GRPC_ADDR`、同じくポート番号のみ)
- 外部公開API: `:8112`(環境変数`EXTERNAL_HTTP_ADDR`)

## アーキテクチャ選定

### (a) HTTPフレームワークにJavalinを選んだ理由(Spring Bootを選ばなかった理由)

3つの案を検討した。

| 案 | 特徴 |
|---|---|
| Spring Boot | DIコンテナ・アノテーション(`@RestController`/`@Autowired`/`@Transactional`)によるconvention over configuration。実務での採用率が最も高い |
| **Javalin(採用)** | Jettyの薄いラッパー。`app.get("/internal/v1/tasks", handler)`のような明示的なルート登録のみで、DIコンテナもアノテーションも無い |
| JDK標準の`com.sun.net.httpserver.HttpServer` | フレームワーク自体が無く、ルーティングも自作する必要がある |

Spring Bootを選ばなかった理由は以下の4点に集約される。

1. **このプロジェクト全体の一貫した方針との相反**: このプロジェクトはORM禁止(生SQLのみ)・C++/Cでの手書きルーター・フレームワークに依存しない設計を貫いており、「フレームワークに頼らず何が起きているか見せる」ことを重視している。Spring BootのDI・アノテーション魔法(`@Autowired`によるフィールド注入、`@RestController`が暗黙に行うJSONシリアライズ、`@Transactional`が暗黙に開始・commit/rollbackするトランザクション境界)は、この方針と正面から相反する
2. **このプロジェクトの認証・Feature Flag設計はSpring Bootの自動設定の前提から外れる**: Spring Securityの`SecurityFilterChain`/`JwtDecoder`は「単一のOAuth2プロバイダ」を前提にした自動設定であり、本実装の3issuer(Keycloak/ローカルHMAC/ローカルRSA)をissで振り分けるDispatcherパターンには活かせない。同様にSpring Data JPAの自動CRUDも、ORM禁止方針(生JDBC限定)のため使えない。Feature Flagポーリング(次フェーズ)も完全に独自実装になる。結局、このプロジェクトで最も学習価値の高い部分(認証・Feature Flag)はSpring Bootを使っても同程度のカスタムコードを書く必要があり、実装効率化の恩恵が限定的
3. **役割の重複**: Spring Bootのような「オピニオネイテッドなフレームワーク」という学習教材の役割は、既にRailsが担っている。同じ役割の教材を重ねて追加する意味は薄い
4. **開発サイクルの体感速度**: JVM起動+Spring ApplicationContext初期化のオーバーヘッドは、Javalinの起動時間と比べて無視できない差になり、開発中の反復サイクル(ビルド→起動→確認)を遅くする

Javalinは実務でも実際に使われている軽量フレームワークであり、「実務で使われている」ことと「明示的で透明である」ことを両立できる選択として採用した。

### (b) gRPC: grpc-java

`grpc-java`はgRPC自体の主要開発言語であり、あらゆる言語のgRPC実装の中でも屈指の成熟度を持つ。生成されたサービススタブ(`TaskServiceGrpc.TaskServiceImplBase`)をそのまま実装するだけで済み、C++/Cで直面したような「ラッパーを選ぶか低レイヤーAPIを直叩きするか」という悩みは生じない。

### (c) DB: 生JDBC + HikariCP(ORM禁止)

`java.sql.PreparedStatement`による生SQL(他言語と統一したORM禁止方針)。コネクションプールは実務で最も広く使われている`HikariCP`を採用した。

### (d) 並行処理モデル: Virtual Threads(Project Loom、Reactiveを選ばなかった理由)

このJava実装で最も学習価値の高い設計判断がこれである。2つの案を検討した。

**採用: Virtual Threads(JDK21+)**
- 見た目は普通の同期的・ブロッキングなコード(`PreparedStatement.executeQuery()`をそのまま呼ぶ)を書くだけでよい
- JVMが「仮想スレッドがI/Oでブロックする瞬間」を検知し、少数のOSキャリアスレッドを他の仮想スレッドへ譲る。JDBCはこの仕組みとネイティブに相性が良く、Node.js/Pythonのように「async専用のDBドライバへの書き換え」が不要
- Goのgoroutine(「ブロッキングに見えるコードで高いスケーラビリティを得る」という思想)と結果は似ているが、実現方式(JVMのcontinuationキャプチャ vs Goの独自M:Nスケジューラ+ネットワークポーラー)が全く異なる好対照になる

**不採用: Reactive(WebFlux + Project Reactor)**
- `Mono`/`Flux`という関数型リアクティブストリームでコードを書く必要があり、学習コスト・デバッグ難易度が非常に高いことで知られる
- **決定的な理由**: `Mono`/`Flux`によるエフェクト合成は、概念的にはbackend-scala-http4sが既に使っている`cats-effect`の`IO`モナドと非常に近く、「純粋関数型エフェクトシステム」という学習テーマが重複してしまう。このプロジェクトで既にカバー済みのテーマを、Java実装でも再び持ち込む価値は薄いと判断した

実装上は、Javalin/Jettyの`QueuedThreadPool`に`setVirtualThreadsExecutor(Executors.newVirtualThreadPerTaskExecutor())`を設定することで、リクエスト処理を仮想スレッドへ委譲している(`Main.java`参照)。実際にリクエストを処理しているスレッドが仮想スレッドであることは、`jcmd <pid> Thread.dump_to_file`で取得したスレッドダンプに`VirtualThread[...]`という形式で現れることで確認できる。

### (e) JWT: 成熟したライブラリ(jjwt)を使う理由

backend-c/backend-cppはOpenSSLのプリミティブを直接使ってJWT検証を自前実装したが、これは「CやC++にはまともなJWTライブラリが無い」というエコシステムの制約から来た選択であり、普遍的な方針ではない(Go/Rust/JS/TSは最初から成熟したライブラリを使っている)。Javaには`jjwt`という成熟したライブラリがあるため、Go/Rust/JS/TSと同じ判断で素直にライブラリを使う。学習予算は代わりにVirtual Threadsの設計に集中させている。

`jjwt`の`Jwts.parser().verifyWith(key)`は鍵の型(HMAC系/RSA系)と整合しないアルゴリズムのトークンをそもそも受理しない設計になっているが、アルゴリズム混同攻撃への多層防御として、ヘッダの`alg`が期待するアルゴリズム(HmacVerifierならHS256、JwksVerifierならRS256)と完全一致することも明示的に確認している(`HmacVerifier`/`JwksVerifier`のコメント参照)。

## 認証について

REST/gRPCともに、本物のJWT/JWKS検証。backend-rust(`src/auth/jwt.rs`)・backend-c/backend-cppと同じ3issuer構成を`com.bffgin.backend.auth`パッケージに実装している。

- **ローカルHMAC**(`iss=bff-gin-local-hmac`): HS256、共有シークレット(`LOCAL_AUTH_HMAC_SECRET`)
- **ローカルRSA**(`iss=bff-gin-local-rsa`): RS256、JWKSはbff自身の`/.well-known/jwks.json`(`LOCAL_AUTH_RSA_JWKS_URL`)から取得
- **Keycloak**: RS256、JWKSはKeycloakの`/protocol/openid-connect/certs`(`KEYCLOAK_ISSUER`から導出、`KEYCLOAK_JWKS_URL`で上書き可)から取得

`Authorization: Bearer <token>`ヘッダ(gRPCは`authorization`メタデータ)を、署名検証前に`iss`だけ覗いて対応するVerifierへ振り分ける`Dispatcher`→実際の署名/`exp`/`aud`検証を行う`HmacVerifier`/`JwksVerifier`という2段構造(他言語と同じ設計)。認証ヘッダが無い、またはいずれの検証にも通らない場合はREST `401 {"error":"unauthorized"}`・gRPC `UNAUTHENTICATED`。

user_id解決(`UserResolver`)は、ローカル発行issuerなら`sub`をそのまま`users.id`として、Keycloak発行issuerなら`sub`(keycloak_sub)を`user_keycloaks`テーブル経由で引く(backend-rustの`resolve_user_id`と同じ設計)。どちらも見つからなければREST `403 {"error":"user_not_provisioned"}`・gRPC `PERMISSION_DENIED`。

JWKSはkid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけ再取得する「kid不一致時のみ再取得」戦略(他言語と同じ)。

## 結合テスト

実DB(docker-compose上のMySQL)に接続する結合テストは、`test`(単体テスト)とは別のGradle source set(`integrationTest`)に分離している。`./gradlew test`単体ではDBが不要で、`./gradlew integrationTest`だけがDBを要求する。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
./gradlew test              # 単体テスト(JWT/Dispatcher/JWKS、DB不要)
./gradlew integrationTest   # 結合テスト(実DB・実gRPCサーバー、上記dockerが必須)
```

### 単体テスト

- `HmacAndDispatcherTest`: ローカルHMAC検証(正常系・不正シークレット・期限切れ・audience不一致・issuer不一致・アルゴリズム混同攻撃対策・`alg:none`拒否)、Dispatcherのissuerルーティング・未知issuer拒否・不正な形式のトークン拒否
- `JwksVerifierTest`: 自プロセス内蔵のモックJWKSサーバー(`com.sun.net.httpserver.HttpServer`)を使い、実際にRSA鍵ペアで署名したRS256トークンの検証・未知kid時の再取得・issuer/audience不一致・期限切れ・アルゴリズム混同攻撃対策を確認する。実際のKeycloak/bffプロセスは不要
- `TaskValidationTest`: name必須・20コードポイント制限(絵文字によるサロゲートペアでの境界値含む)・finished_on過去日禁止・不明なstatus拒否
- `RestErrorMapperTest`: 全エラー種別がbackend-rustのerror.rsと同じHTTPステータス・JSON形状にマッピングされることを確認

### 結合テスト

- `TaskRepositoryIntegrationTest`: create/find/update/delete一連の往復、delete時の`task_labels`孤立行防止(トランザクション保護)、create/update双方での重複label_idの正規化、他ユーザーのtaskへの不可視性(所有権分離)、存在しないtaskの更新/削除がfalseを返すこと、offsetページングの`total`/件数、cursorページングのid昇順・`afterId`境界、`findUserById`/`findUserByKeycloakSub`
- `TaskGrpcIntegrationTest`: 実際に`grpc-java`のサーバーをテスト用ポートで起動し、生成された`TaskServiceGrpc.TaskServiceBlockingStub`から実際にRPCを送る。create/get/update/delete一連の往復、delete時の`task_labels`孤立行防止、`authorization`メタデータ無しの呼び出しが`UNAUTHENTICATED`になること、期限切れJWTでの呼び出しが`UNAUTHENTICATED`になること、cursorページングの2ページ目・「次ページ無し」判定

いずれもテストごとに一意なemail/keycloak_sub/ラベル名でデータを作り(`DbTestFixture`)、`@AfterEach`で後始末することで共有の開発用DBを汚さない。

## セットアップ

```sh
brew install openjdk gradle   # 初回のみ(JDK 21以上。OpenJDKはPATHに無いことがあるため下記export推奨)
export JAVA_HOME=/opt/homebrew/opt/openjdk
export PATH="$JAVA_HOME/bin:$PATH"

./gradlew build
./gradlew run
```

DB接続・JWT関連の既定値はbackend-rust/backend-c/backend-cppと同じ環境変数名(`DB_HOST`/`DB_PORT`/`DB_USER`/`DB_PASSWORD`/`DB_SCHEMA`、`KEYCLOAK_ISSUER`、`EXPECTED_AUDIENCE`、`LOCAL_AUTH_HMAC_SECRET`、`LOCAL_AUTH_RSA_JWKS_URL`)を使う。

Rust/Scala×2/Rails/JavaScript/TypeScript/C++/Cと同じく、backend-java自身は独自のマイグレーションを持たない。スキーマの正本は`backend/migrations`のみで、同じMySQL(`bff_gin_development`)を読み書きするだけ。

## 動作確認(実機で確認済み)

- `./gradlew build`: 通ることを確認済み
- `./gradlew test`(単体、DB不要): 全件pass
- `./gradlew integrationTest`(実DB・実gRPC): 全件pass
- 実機(docker-compose上のMySQL・Keycloak)に対して、実際に`HTTP_ADDR=8111`/`GRPC_ADDR=9101`でサーバーを起動し、以下を`curl`/`grpcurl`で確認済み:
  - 認証ヘッダ無し → REST `401 {"error":"unauthorized"}`/gRPC `UNAUTHENTICATED`
  - 実際に署名した有効なローカルHMAC JWT → CRUD一連の往復が正しいJSON/protobuf形状で動作
  - 期限切れのローカルHMAC JWTが正しく拒否されること
  - 実際にKeycloakから取得したClient Credentials Grantトークン(RS256)がJWKS経由で正しく署名検証され(`user_not_provisioned`まで到達することで検証成功を確認、このクライアントはusersに存在しないため)
  - `label_ids: [7,7,8]`のような重複が正規化されて2件になること(REST/gRPC双方)
  - gRPCのcursorページングが2ページ目・「次ページ無し」まで正しく動くこと
  - Virtual Threadsが実際にリクエストを処理していることをスレッドダンプで確認済み

## ログ

REST v1・外部公開APIともgRPCと同じkey=value形式でリクエスト単位のログを標準出力に出す(いずれも`log.info`、`method`/`path`/`status`/`duration_ms`フィールド)。Javalinの`before`/`after`フックで実装しており、`after`はJavalinの例外ハンドラより後に実行されるため、`TaskError`から`RestErrorMapper`が書き込んだ実際のステータスコードを`ctx.status()`で正しく取得できる。

- REST: `rest method=GET path=/internal/v1/tasks status=200 duration_ms=5`
- 外部公開API: `external method=GET path=/external/v1/tasks status=401 duration_ms=0`
- gRPC: `grpc method=list_tasks status=OK duration_ms=3`(既存)

`LOG_LEVEL`環境変数(`debug`/`info`/`warn`/`error`、既定`info`、backend(Go)/bff/gateway/goと同じ環境変数名)でログレベルを切り替えられる。`debug`にすると、上記のINFOサマリ行に加えて`UserResolver`(user_id解決の詳細)・`JwksVerifier`(JWKS再取得イベント)がDEBUGレベルの詳細ログを追加で出す。`slf4j-simple`がプロセス内で最初に`Logger`を取得する前にシステムプロパティ`org.slf4j.simpleLogger.defaultLogLevel`を設定する必要があるため、`Main`クラスの`log`フィールドより前の`static`初期化子でこの環境変数を読んでいる。

## 実装時に判明した既知の差異(実機検証結果)

- **`Timestamp#toLocalDateTime()`はJVMのシステムデフォルトタイムゾーンで変換される**: MySQL Connector/Jの`ResultSet#getTimestamp()`が返す`java.sql.Timestamp`に対して`toLocalDateTime()`を呼ぶと、内部的に`ZoneId.systemDefault()`(このプロジェクトの開発機ではJST、UTC+9)を経由して変換されるため、実際にはUTCの壁時計値が格納されているDATETIME列から9時間ずれた値を読んでしまう(実機検証で発覚: 他言語が`10:33:15`と表示する行が`19:33:15`になっていた)。修正: `ResultSet#getObject(column, LocalDateTime.class)`を使う。これは列の生の日時要素をタイムゾーン変換無しでそのまま`LocalDateTime`にマッピングするため、DATETIME列(タイムゾーン情報を持たない)を扱う際の正しい方法になる。書き込み側も同様に`PreparedStatement#setObject(index, localDateTime)`を使い、`Timestamp.valueOf`/`setTimestamp`経由のタイムゾーン変換を避けている
- **protobuf-javaのバージョンはgRPC本体の推移的依存で決まる**: `com.google.protobuf:protoc`に指定したバージョン(3.25.x)と、`grpc-services`等が要求する実際のprotobuf-javaのバージョン(4.26.x、`GeneratedMessageV3`から`GeneratedMessage`への破壊的変更を含む)が食い違うと生成コードがコンパイルできない。`./gradlew dependencies --configuration compileClasspath`で実際に解決されるバージョンを確認し、`protoc`/`protobuf-java`双方をそのバージョンに合わせる必要がある
- **Javalin 6のJetty仮想スレッド設定は`JettyConfig#threadPool`フィールドへの直接代入**: `server(Supplier<Server>)`のような完全カスタマイズ用メソッドは無く、`cfg.jetty.threadPool = customThreadPool`という単純なフィールド代入で設定する

## 外部公開API・Feature Flagポーリング

`/external/v1/tasks`(:8112配下、CONTRACT.mdセクション11)は、bffを経由しないClient Credentials Grant専用のエンドポイント。内部REST/gRPCと同じ`Dispatcher`で署名検証まで行うが、`ExternalAuth.requireExternalClient`がさらに2点を追加で要求する: (1) ローカル(HMAC/RSA)発行のissuerは拒否する(Keycloak発行のみ受け付ける)、(2) `azp`クレームが`EXTERNAL_API_CLIENT_ID`(既定`external-api-client`)と一致すること。`user_id`はクエリパラメータの値をそのまま信頼する(サーバー間の信頼関係を前提にした設計)。

- v1(既定、`backend.external-tasks-pagination-v2`がOFF): `page`/`page_size`によるoffsetページング。既存の`TaskRepository.listOffset`を内部REST v1とそのまま共用する
- v2(同フラグがON): `cursor`/`limit`によるid昇順のkeysetページング。既存の`TaskRepository.listCursor`(内部gRPC v2と共用)をそのまま使う。cursorは最後の行の`id`をそのまま文字列化したもの(合成キーは使わない)
- レスポンスのTask JSON形状は内部REST v1と完全に同じ(`TaskJson.toJson`を共有、`user_id`を含まない)

`FeatureFlagPoller`は`feature_flags`テーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、backend-c/backend-cpp/backend-rustと同じ設計)。読み取り側がロック無しで安全に読めるよう、キャッシュ全体を不変Mapとしてvolatileフィールドへ丸ごと差し替える設計にしている(ポーリングスレッドだけが書き込み側になり、他スレッドは常に一貫したスナップショットを読む)。1回のポーリング失敗(DB接続断等)で例外を伝播させスケジューラ自体を止めてしまうと、以後永久にフラグが更新されなくなるため、失敗時は直前の正常なキャッシュを保持したまま次回に委ねる。

## 動作確認(外部公開API、実機で確認済み)

- Keycloakから実際に`client_credentials`グラントで取得したトークン(`azp=external-api-client`)で`GET /external/v1/tasks?user_id=1`を呼び、`200`で既存データが正しいJSON形状(offsetページング)で返ることを確認済み
- 実際に署名したローカルHMACトークン(`azp`が偶然一致していても)で同エンドポイントを呼ぶと`401`で拒否されることを確認済み(Client Credentials Grant以外は受け付けない設計が実際に機能している)
- 認証ヘッダ無し→`401`、`user_id`欠如→`400 user_id_required`、`user_id`が数値でない→`400 invalid_user_id`を確認済み

## 未実装・今後の予定

- bff/gateway/migrationへの配線(`backend.task-language`への`java`の追加)
