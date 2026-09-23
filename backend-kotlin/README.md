# backend-kotlin

bff-gin: Task CRUD backendのKotlin実装(CONTRACT.mdセクション20の多言語backend比較の11言語目)

**このフェーズのスコープ**: 内部REST v1・内部gRPC v2のCRUD、JWT/JWKS認証(ローカルHMAC/ローカルRSA/Keycloakの3issuer)、外部公開API、Feature Flagポーリング。bff/gateway/migrationへの配線は未実装(下記「未実装・今後の予定」参照)。

## ポート

- 内部REST v1: `:8113`(環境変数`HTTP_ADDR`。他言語と同じくポート番号のみを受け付ける簡略形式)
- 内部gRPC v2: `:9102`(環境変数`GRPC_ADDR`、同じくポート番号のみ)
- 外部公開API: `:8114`(環境変数`EXTERNAL_HTTP_ADDR`、同じくポート番号のみ)

## アーキテクチャ選定

### (a) HTTPフレームワークにKtorを選んだ理由(Spring Bootを選ばなかった理由、Javalinを流用しなかった理由)

3つの案を検討した。

| 案 | 特徴 |
|---|---|
| Spring Boot(Kotlinサポート含む) | DIコンテナ・アノテーション(`@RestController`/`@Autowired`/`@Transactional`)によるconvention over configuration。`suspend fun`ハンドラのサポートはあるが、DIコンテナ自体は変わらない |
| **Ktor(採用)** | JetBrains製、コルーチンをゼロから前提に設計されたKotlinネイティブフレームワーク。`routing { get("/internal/v1/tasks") { ... } }`という明示的なルーティングDSL、アノテーション無し、ハンドラは`suspend fun` |
| backend-javaのJavalinをそのまま流用 | 実装コストは最も低いが、Javalin自体はブロッキングI/Oを前提にしたJava製フレームワークであり、コルーチンネイティブなフレームワークで書く経験そのものが失われる |

Spring Bootを選ばなかった理由はbackend-javaと同じ4点に集約される。

1. **このプロジェクト全体の一貫した方針との相反**: ORM禁止(生SQLのみ)・C++/Cでの手書きルーター・フレームワークに依存しない設計を貫いており、「フレームワークに頼らず何が起きているか見せる」ことを重視している。Spring BootのDI・アノテーション魔法は、この方針と正面から相反する
2. **このプロジェクトの認証・Feature Flag設計はSpring Bootの自動設定の前提から外れる**: Spring Securityの自動設定は「単一のOAuth2プロバイダ」を前提にしており、本実装の3issuer(Keycloak/ローカルHMAC/ローカルRSA)をissで振り分けるDispatcherパターンには活かせない。Spring Data JPAの自動CRUDも、ORM禁止方針のため使えない。結局、このプロジェクトで最も学習価値の高い部分はSpring Bootを使っても同程度のカスタムコードが必要になり、実装効率化の恩恵が限定的
3. **役割の重複**: Spring Bootのような「オピニオネイテッドなフレームワーク」という学習教材の役割は、既にRailsが担っている
4. **開発サイクルの体感速度**: JVM起動+Spring ApplicationContext初期化のオーバーヘッドは、Ktorの起動時間と比べて無視できない差になる

Javalinを流用しなかった理由はこのKotlin実装固有のものである。Kotlinを追加する一番の狙いは「コルーチンネイティブなフレームワークを実際に体験すること」であり、Java製の(ブロッキングI/O前提の)フレームワークにコルーチンを後付けで接続するのでは、この狙いが達成できない。KtorはCIOエンジン(純Kotlin実装、コルーチンとの親和性が高い)を採用し、ルーティングDSLからハンドラの中身まで一貫して`suspend fun`で書く。

### (b) gRPC: grpc-kotlin

`grpc-kotlin`は`grpc-java`の上に構築された公式ライブラリで、`TaskServiceCoroutineImplBase`(サーバー側)・`TaskServiceCoroutineStub`(クライアント側)という、`suspend fun`ベースの生成コードを提供する。backend-javaで確立した`grpc-java`の基盤(同じ`.proto`、同じメッセージ型)をそのまま拡張でき、エコシステムリスクはほぼゼロ。C++/Cで直面したような「ラッパーを選ぶか低レイヤーAPIを直叩きするか」という悩みも生じない。

grpc-kotlinのCoroutineImplBaseは、grpc-java(StreamObserverベースのコールバックAPI)と違い、`suspend fun`がそのままレスポンスを返す/例外を投げるだけでよく、手動で`onNext`/`onCompleted`を呼ぶ必要が無い。

### (c) DB: 生JDBC + HikariCP(ORM禁止)

`java.sql.PreparedStatement`による生SQL(他言語と統一したORM禁止方針)。コネクションプールはbackend-javaと同じ`HikariCP`を採用した。

### (d) 並行処理モデル: 構造化並行性と明示的なディスパッチャ選択(Javaとの意図的な対比)

**このKotlin実装で最も学習価値の高い設計判断**であり、13言語構成の中でKotlinが担うべき一番の役割でもある。

backend-java(Virtual Threads、JDK21+)は「並行処理の安全性を自動化する」設計だった。JDBCのような普通のブロッキング呼び出しをそのまま書いても、JVMが「仮想スレッドがI/Oでブロックする瞬間」を自動的に検知し、少数のOSキャリアスレッドを他の仮想スレッドへ譲ってくれる。呼び出し側(`TaskRepository`相当のクラス)には特別な記述が一切不要だった。

このKotlin実装は対照的に、**「並行処理の安全性を型システムと明示的なディスパッチャ選択(`withContext(Dispatchers.IO)`)で保証する」設計**を採っている。

- `TaskRepository`の全メソッドは、JDBC呼び出しを行う本体を`withContext(Dispatchers.IO) { ... }`で明示的に囲んでいる(`TaskRepository.kt`参照)。これを怠ると、JDBCの同期呼び出しがコルーチンのデフォルトディスパッチャ(限られたスレッド数)を専有し、他の無関係なコルーチン全体が詰まってしまう。つまりKotlinでは「これはブロッキングI/Oである」と呼び出し側が自己申告する必要があり、Javaのような自動化は無い
- JWKS取得(`JwksVerifier`)も、`java.net.http.HttpClient`の同期呼び出しではなくKtor Client(`suspend fun get(...)`)を使うことで、ルーティングからHTTP呼び出しまでコルーチンネイティブな一貫性を保っている。1箇所でも同期呼び出しを混ぜると、その箇所だけディスパッチャのスレッドを実際にブロックしてしまい、設計全体の一貫性が崩れる
- **構造化並行性**: Ktorは各リクエストを、そのリクエストのライフサイクルに紐づくコルーチンスコープ上で処理する。クライアントが接続を切断すればそのスコープがキャンセルされ、配下で起動された子コルーチンにもキャンセルが伝播する。これはbackend-javaのスレッド割り込みに基づく、より粗い取り消しモデルとは対照的な、Kotlinコルーチン特有の精密な取り消し伝播モデルである(`Main.kt`のstartRestServer関数参照)

同じJVM上に実装されたbackend-cppの`asio::thread_pool`によるブロッキングDB呼び出しの隔離(README.md「同期DBアクセスの隔離」節参照)も、「非同期ランタイムからブロッキング呼び出しを明示的に隔離する」という同じ主題の別実装だが、C++は自作のスレッドプールで実現しているのに対し、Kotlinは言語・ランタイムが標準で提供する`Dispatchers.IO`という薄い抽象で同じことを実現している点が対照的である。

### (e) JWT: 成熟したライブラリ(jjwt)を使う理由

backend-javaと同じ判断で、`jjwt`という成熟したライブラリをそのまま使う。KotlinはJavaと完全な相互運用性があるため追加コストは無い。学習予算はこのKotlin実装固有のテーマ(構造化並行性・明示的なディスパッチャ選択)に集中させている。

`jjwt`の`Jwts.parser().verifyWith(key)`は鍵の型(HMAC系/RSA系)と整合しないアルゴリズムのトークンをそもそも受理しない設計になっているが、アルゴリズム混同攻撃への多層防御として、ヘッダの`alg`が期待するアルゴリズム(HmacVerifierならHS256、JwksVerifierならRS256)と完全一致することも明示的に確認している(`HmacVerifier`/`JwksVerifier`のコメント参照)。

## 認証について

REST/gRPCともに、本物のJWT/JWKS検証。backend-java(`com.bffgin.backend.auth`)・backend-rust(`src/auth/jwt.rs`)と同じ3issuer構成を実装している。

- **ローカルHMAC**(`iss=bff-gin-local-hmac`): HS256、共有シークレット(`LOCAL_AUTH_HMAC_SECRET`)
- **ローカルRSA**(`iss=bff-gin-local-rsa`): RS256、JWKSはbff自身の`/.well-known/jwks.json`(`LOCAL_AUTH_RSA_JWKS_URL`)から取得
- **Keycloak**: RS256、JWKSはKeycloakの`/protocol/openid-connect/certs`(`KEYCLOAK_ISSUER`から導出、`KEYCLOAK_JWKS_URL`で上書き可)から取得

`Authorization: Bearer <token>`ヘッダ(gRPCは`authorization`メタデータ)を、署名検証前に`iss`だけ覗いて対応するVerifierへ振り分ける`Dispatcher`(suspend fun)→実際の署名/`exp`/`aud`検証を行う`HmacVerifier`/`JwksVerifier`という2段構造(他言語と同じ設計)。認証ヘッダが無い、またはいずれの検証にも通らない場合はREST `401 {"error":"unauthorized"}`・gRPC `UNAUTHENTICATED`。

user_id解決(`UserResolver`)は、ローカル発行issuerなら`sub`をそのまま`users.id`として、Keycloak発行issuerなら`sub`(keycloak_sub)を`user_keycloaks`テーブル経由で引く。どちらも見つからなければREST `403 {"error":"user_not_provisioned"}`・gRPC `PERMISSION_DENIED`。

JWKSはkid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけ再取得する「kid不一致時のみ再取得」戦略(他言語と同じ)。再取得処理は`Mutex`(コルーチン向けの排他制御、`synchronized`はsuspend関数内では使えないため)で保護している。

## 外部公開API・Feature Flagポーリング

- `flags/FeatureFlagPoller.kt`: `feature_flags`テーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、backend-java/backend-rust/backend-cpp/backend-cと同じ設計)。**このKotlin実装の一貫した設計原則をポーラーにも適用**: バックグラウンド処理だからといって`withContext(Dispatchers.IO)`による明示的なブロッキングI/O隔離を省略しない(`TaskRepository`と同じ規律)。キャッシュは`@Volatile`な不変Mapを毎回丸ごと置き換える方式で、読み取り側にロックは不要
- `auth/ExternalAuth.kt`: `GET /external/v1/tasks`用のRequireExternalClientAuth相当。通常のDispatcherで署名検証した後、(1)issがローカル(HMAC/RSA)発行なら拒否(Client Credentials Grant、つまりKeycloak発行分のみ許可)、(2)`azp`クレームが`EXTERNAL_API_CLIENT_ID`(既定`external-api-client`)と一致しなければ拒否する。`user_id`はクエリパラメータをそのまま使い、JWTの`sub`とは突き合わせない(この資格情報を持つ者は任意ユーザーのタスクを読み取れる、CONTRACT.mdセクション11に明記された既知の設計)
- `rest/ExternalRoutes.kt`: `backend.external-tasks-pagination-v2`でoffset(v1、既定、`page`/`page_size`/`total`)/cursor(v2、`cursor`/`limit`/`next_cursor`、id昇順のkeyset pagination)を切り替える。既存の内部REST v1と同じ`TaskRepository`の`listOffset`/`listCursor`をそのまま再利用しており、新規のクエリロジックは書いていない
- `Claims`データクラスに`azp`フィールドを追加した(Phase 1では内部user_id解決に不要なため省略していた)。`HmacVerifier`/`JwksVerifier`双方の署名検証成功パスで、jjwtの`claims.get("azp", String::class.java)`から取得する

## ログ出力・LOG_LEVEL

内部REST v1(`:8113`)・外部公開API(`:8114`)ともに、`grpc method=...`(`TaskGrpcService.kt`)と同じキー=値形式で、1リクエスト1行の要約ログをINFOレベルで出す(`rest method=GET path=/internal/v1/tasks status=200 duration_ms=12`のような形)。

- **実装方法**: `Main.kt`の`installRequestLogging()`が、REST/外部それぞれの`embeddedServer`に対して`intercept(ApplicationCallPipeline.Monitoring) { ... proceed() ... }`を明示的に登録する。KtorのCallLoggingプラグインではなくこの形にしたのは、Phoenixではなく素のPlug相当のKtorを選んだのと同じ「フレームワークの魔法に頼らず何が起きているか見せる」方針による
- **`LOG_LEVEL`環境変数**(既定`info`): `debug`/`info`/`warn`/`error`を指定できる(backend(Go)/bff/gateway/goと同じ命名)。`debug`にすると、上記の要約行に加え、`UserResolver`が解決した`user_id`・issuer、`JwksVerifier`のkidキャッシュヒット/ミス・JWKS再取得といった、リクエストの内部動作がより詳細に出力される
- **ロギングバックエンドはlogback-classic**(slf4j-simpleではない): `src/main/resources/logback.xml`の`<root level="${LOG_LEVEL:-INFO}">`が、OS環境変数を設定ファイル読み込み時に直接参照する。slf4j-simpleはこの手のレベル設定をクラスロード時に一度だけ静的に確定してしまい、Kotlin側のコードでの`System.setProperty(...)`がクラスロード順序に左右されて信頼できないため、logback-classicへ切り替えた

## 結合テスト

実DB(docker-compose上のMySQL)に接続する結合テストは、`test`(単体テスト)とは別のGradle source set(`integrationTest`)に分離している。`./gradlew test`単体ではDBが不要で、`./gradlew integrationTest`だけがDBを要求する。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
./gradlew test              # 単体テスト(JWT/Dispatcher/JWKS/バリデーション、DB不要)
./gradlew integrationTest   # 結合テスト(実DB・実gRPCサーバー、上記dockerが必須)
```

### 単体テスト

- `HmacAndDispatcherTest`: ローカルHMAC検証(正常系・不正シークレット・期限切れ・audience不一致・issuer不一致・アルゴリズム混同攻撃対策・`alg:none`拒否)、Dispatcherのissuerルーティング・未知issuer拒否・不正な形式のトークン拒否
- `JwksVerifierTest`: 自プロセス内蔵のモックJWKSサーバー(`com.sun.net.httpserver.HttpServer`、テスト専用の道具)を使い、実際にRSA鍵ペアで署名したRS256トークンの検証・未知kid時の再取得・issuer/audience不一致・期限切れ・アルゴリズム混同攻撃対策を確認する。実際のKeycloak/bffプロセスは不要
- `TaskValidationTest`: name必須・20コードポイント制限(絵文字によるサロゲートペアでの境界値含む)・finished_on過去日禁止・不明なstatus拒否
- `RestErrorMapperTest`: 全エラー種別がbackend-java/backend-rustと同じHTTPステータス・JSON形状にマッピングされることを確認
- `ExternalQueryTest`: 外部公開APIのクエリパラメータ解析(`user_id`必須・数値検証、`page`/`page_size`の既定値・1未満のクランプ、`cursor`/`limit`の既定値・0以下のcursorを「未指定」として扱う変換)を実サーバー無しで検証
- `ExternalAuthTest`: `azp`一致トークンの受理、`Authorization`ヘッダ無し・`azp`不一致・`azp`欠如の拒否、**ローカルHMAC発行トークン(署名・iss・aud・azpが全て正しくても、issがローカルという理由だけで拒否されること)**をモックJWKSサーバーで検証

いずれもsuspend funを呼ぶテストは`kotlinx-coroutines-test`の`runTest`で実行する。

### 結合テスト

- `TaskRepositoryIntegrationTest`: create/find/update/delete一連の往復、delete時の`task_labels`孤立行防止(トランザクション保護)、create/update双方での重複label_idの正規化、他ユーザーのtaskへの不可視性(所有権分離)、存在しないtaskの更新/削除がfalseを返すこと、offsetページングの`total`/件数、cursorページングのid昇順・`afterId`境界、`findUserById`/`findUserByKeycloakSub`
- `TaskGrpcIntegrationTest`: 実際に`grpc-kotlin`のサーバーをテスト用ポートで起動し、生成された`TaskServiceCoroutineStub`から実際にRPCを送る。create/get/update/delete一連の往復、delete時の`task_labels`孤立行防止、`authorization`メタデータ無しの呼び出しが`UNAUTHENTICATED`になること、期限切れJWTでの呼び出しが`UNAUTHENTICATED`になること、cursorページングの2ページ目・「次ページ無し」判定
- `FeatureFlagPollerIntegrationTest`: 実DBに一意なテスト専用`flag_key`の行を挿入し、`pollOnce()`直後に`variation()`のフォールバック規則(enabled=true→default_variation、enabled=false→呼び出し側のdefault_value、flag_key未登録→同じくdefault_value)を検証する。他機能が参照する既存のflag行(`backend.external-tasks-pagination-v2`等)には一切触れない
- `ExternalHandlerIntegrationTest`: 実DB・モックJWKSサーバー(Keycloak相当として扱う)・外部公開API自身のKtorリスナー(テスト用ポート)の3つを1プロセス内に立て、実際にHTTP GETを送ってエンドツーエンドに検証する。`Authorization`ヘッダ無し→401、`azp`不一致→401、`user_id`欠如→400、v1(offset)ページングがページ境界をまたいで正しく動くこと、v2(cursor)ページングが`next_cursor`を正しく連鎖させ最終ページで`null`になること。`backend.external-tasks-pagination-v2`は退避→書き換え→テスト後に復元する(全言語で共有する1つのFeature Flagのため、他のテスト・実機動作に影響を残さない)

実DBを相手にする結合テストは`kotlinx.coroutines.runBlocking`で実行する(仮想時間で進む`runTest`ではなく、実際の壁時計時間でDB/ネットワークとやり取りする必要があるため)。いずれもテストごとに一意なemail/keycloak_sub/ラベル名でデータを作り(`DbTestFixture`)、`@AfterEach`で後始末することで共有の開発用DBを汚さない。

## セットアップ

```sh
brew install openjdk gradle   # 初回のみ(JDK 21以上。OpenJDKはPATHに無いことがあるため下記export推奨)
export JAVA_HOME=/opt/homebrew/opt/openjdk
export PATH="$JAVA_HOME/bin:$PATH"

./gradlew build
./gradlew run
```

Kotlinコンパイラ自体は別途インストール不要(GradleのKotlin JVMプラグインが自身のツールチェーンでコンパイラを管理する)。

DB接続・JWT関連の既定値はbackend-java/backend-rust/backend-c/backend-cppと同じ環境変数名(`DB_HOST`/`DB_PORT`/`DB_USER`/`DB_PASSWORD`/`DB_SCHEMA`、`KEYCLOAK_ISSUER`、`EXPECTED_AUDIENCE`、`LOCAL_AUTH_HMAC_SECRET`、`LOCAL_AUTH_RSA_JWKS_URL`)を使う。

他言語と同じく、backend-kotlin自身は独自のマイグレーションを持たない。スキーマの正本は`backend/migrations`のみで、同じMySQL(`bff_gin_development`)を読み書きするだけ。

## 動作確認(実機で確認済み)

- `./gradlew build`: 通ることを確認済み
- `./gradlew test`(単体、DB不要): 全件pass
- `./gradlew integrationTest`(実DB・実gRPC): 全件pass
- 実機(docker-compose上のMySQL・Keycloak)に対して、実際に`HTTP_ADDR=8113`/`GRPC_ADDR=9102`でサーバーを起動し、以下を`curl`/`grpcurl`で確認済み:
  - 認証ヘッダ無し → REST `401 {"error":"unauthorized"}`/gRPC `UNAUTHENTICATED`
  - 実際に署名した有効なローカルHMAC JWT → CRUD一連の往復が正しいJSON/protobuf形状で動作
  - 期限切れのローカルHMAC JWTが正しく拒否されること
  - `label_ids: [7,7,8]`のような重複が正規化されて2件になること(REST/gRPC双方)
  - gRPCのcursorページングが2ページ目・「次ページ無し」まで正しく動くこと
- `withContext(Dispatchers.IO)`が実際に効いていることは、`coroutineContext[CoroutineDispatcher]`を直接ログ出力して確認した(**スレッド名の比較では確認できない**: kotlinx.coroutinesの`Dispatchers.IO`と`Dispatchers.Default`は同じ`CoroutineScheduler`のワーカースレッドプールを共有しており、スレッド名は両方とも`DefaultDispatcher-worker-*`になる。ディスパッチャの区別はスレッド名ではなく、`CoroutineDispatcher`インスタンス自体の同一性で行う必要がある)。**実機検証で判明した副次的な事実**: KtorのCIOエンジンは、そもそもルートハンドラの呼び出し時点で既にアンビエントディスパッチャとして`Dispatchers.IO`自体を使っている(`coroutineContext[CoroutineDispatcher]`のログ出力で確認)。そのため`TaskRepository`内の`withContext(Dispatchers.IO)`は、CIOエンジン単体で見ると同じディスパッチャへの再入になる。それでも以下の理由でこの設計は妥当である: (1) Ktorのエンジン(CIO/Netty/Jetty)ごとにアンビエントディスパッチャの既定値は異なりうる仕様上の実装詳細であり、それに依存せず「JDBCはブロッキングI/Oである」と`TaskRepository`自身が明示するのが、どのエンジンを選んでも安全な設計になる、(2) 学習教材としては、たとえ結果的に同じディスパッチャに再入するとしても「呼び出し側の善意(=たまたまアンビエントがIOだったこと)に頼らず、ブロッキングI/Oを行うコード自身がそれを自己申告する」という設計原則そのものに価値がある

## 実装時に判明した既知の差異(実機検証結果)

- **`main()`がそのまま返るとJVMプロセスごと終了する**: backend-javaはJetty(Virtual Threads)とgrpc-javaの両方が非デーモンスレッドを保持するため、`main()`が両サーバーを起動してすぐ返ってもプロセスは生き続けた。Kotlin実装では、KtorのCIOエンジンがkotlinx.coroutinesのディスパッチャ(デーモンスレッド)上で動くため、`main()`が単純に返ると(grpc-javaの非デーモンスレッドの有無に関わらず)実機検証でプロセスが即座に終了する事象を確認した。grpc-java公式サンプルと同じ`grpcServer.awaitTermination()`で`main()`のメインスレッドを明示的にブロックすることで解決した(`Main.kt`参照)
- **`ResultSet#getTimestamp().toLocalDateTime()`はJVMのシステムデフォルトタイムゾーンで変換される**: backend-javaで見つかったのと同じ落とし穴。MySQL Connector/Jの`ResultSet#getTimestamp()`が返す`java.sql.Timestamp`に対して`toLocalDateTime()`を呼ぶと、内部的に`ZoneId.systemDefault()`(このプロジェクトの開発機ではJST、UTC+9)を経由して変換されるため、実際にはUTCの壁時計値が格納されているDATETIME列から9時間ずれた値を読んでしまう。修正: `ResultSet#getObject(column, LocalDateTime::class.java)`を使う(`TaskRepository.kt`のrowToTask参照)
- **`synchronized`はsuspend関数内で使えない**: JavaのJwksVerifierは`synchronized`メソッドでJWKS再取得を排他制御していたが、Kotlinの`synchronized`はブロック内でのsuspend呼び出しをコンパイルエラーにする(スレッドを保持したまま一時停止することの安全性をコンパイラが保証できないため)。`kotlinx.coroutines.sync.Mutex`の`withLock`を代わりに使う
- **grpc-kotlinはCoroutineStubの失敗を`StatusRuntimeException`ではなく`StatusException`(検査例外)として投げる**: grpc-javaの`TaskServiceBlockingStub`とは例外の型が異なるため、クライアント側のテストコードで捕捉する例外の型に注意が必要

## 未実装・今後の予定

- bff/gateway/migrationへの配線(`backend.task-language`への`kotlin`の追加)
- bff/gateway/migrationへの配線(`backend.task-language`への`kotlin`の追加)
