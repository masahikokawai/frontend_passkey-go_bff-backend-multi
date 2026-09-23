# backend-elixir

bff-gin: Task CRUD backendのElixir実装(CONTRACT.mdセクション20の多言語backend比較の13言語目、最後の追加言語)

**スコープ**: 内部REST v1・内部gRPC v2・JWT/JWKS認証(ローカルHMAC/ローカルRSA/Keycloakの3issuer)・外部公開API・Feature Flagポーリング・bff/gateway/migrationへの配線(`backend.task-language`への`elixir`追加、migration `000019`)。

## ポート

- 内部REST v1: `:8117`(環境変数`HTTP_ADDR`)
- 内部gRPC v2: `:9104`(環境変数`GRPC_ADDR`)
- 外部公開API: `:8118`(環境変数`EXTERNAL_HTTP_ADDR`)

## アーキテクチャ選定

### (a) HTTPフレームワークにPlug+Cowboyを選んだ理由(Phoenixを選ばなかった理由)

2つの案を検討した。

| 案 | 特徴 |
|---|---|
| Phoenix | LiveView・Channels(WebSocket)・Ecto統合・ジェネレータ・contextsパターンを持つElixirの主流フレームワーク |
| **Plug + Cowboy(直接利用、採用)** | `Plug.Router`のルーティングマクロ(`get "/internal/v1/tasks" do ... end`)で明示的にルートを登録するのみ。コントローラもジェネレータも無い |

Phoenixを選ばなかった理由は、Java実装でSpring Bootを、Kotlin実装でJavalin流用ではなくKtorを、Python実装でFlask/生Starletteではなく手書きバリデーション付きFastAPIを選んだのと同じ一貫した方針による。Phoenixのような「オピニオネイテッドなフレームワーク」という学習教材の役割は、既にRailsが担っている。同じ役割の教材をElixirでも重ねて追加する意味は薄く、加えてLiveView・Channels・アセットパイプラインといったPhoenixの主要な差別化要素は、今回の純粋なJSON APIには一切不要である。Plug+Cowboyは実務でも実際に使われている組み合わせであり、「実務で使われている」ことと「明示的で透明である」ことを両立できる選択として採用した。

### (b) DB: Ecto(このプロジェクトの「ORM禁止」方針に対する意図的な例外)

このプロジェクトは他の追加言語(Rust/Scala×2/JS/TS/C++/C/Java/Kotlin/Python)全てで「生SQL、ORM禁止」を貫いてきたが、Elixir実装ではElixirコミュニティの標準的作法である**Ecto**(`Ecto.Repo`/`Ecto.Schema`/`Ecto.Changeset`)を採用する。これは見落としではなく、ユーザーとの合意に基づく意図的な例外である。

ただし、ビジネスルールの検証(名前の20コードポイント制限・カレンダー上の日付妥当性・statusのenum判定・過去日判定)は、他言語の`TaskValidation`/`validate_task_input`相当と同じ、`BackendElixir.Domain.Validation`という独立した明示的な関数として書く(`Ecto.Changeset`の`validate_length`等のバリデーションDSLには任せない)。`Ecto.Changeset`は構造的な型変換(パラメータ→型付き構造体)にのみ用いる。これにより、Ectoを採用しつつも「ビジネスルールの検証ロジックを言語間で見比べられる」というこのプロジェクトの比較手法の一貫性は保っている。

副次的な効果として、Rails(ActiveRecord)・Go v1(GORM)に続く3つ目の異なるORM設計思想(Changesetベースの明示的な検証パイプライン)が比較対象に加わることになる。

### (c) gRPC: elixir-grpc(受け入れたエコシステムリスク)

Hexの`grpc`パッケージ(elixir-grpc)+`protoc-gen-elixir`を使う。正直に言うと、これが4言語(Java/Kotlin/Python/Elixir)の中で唯一エコシステムリスクが残る部分である。`grpc-java`/`grpc-kotlin`/`grpcio`ほどの実績・成熟度は無い。単純なunary RPC(ストリーミング無し、このプロジェクトが必要とする範囲)であれば実用上問題なく動作することを実機で確認済みだが、ライブラリ自体の枯れ具合はJVM/Python勢に及ばない。

### (d) 並行処理モデル: GenServer・Supervisor・BEAMのプリエンプティブスケジューラ(13言語構成の最後を締めくくるテーマ)

**このElixir実装で最も学習価値の高い設計判断**であり、Java(Virtual Threads、自動化)・Kotlin(`Dispatchers.IO`、明示的なディスパッチャ選択)・Python(`aiomysql`、ドライバネイティブ)に続く、第4のまったく異なるモデルを提示する。

**JWKS鍵キャッシュをGenServerで実装する(ロックを使わない設計)**: `backend-kotlin`はkidごとの公開鍵キャッシュを`Mutex`(kotlinx.coroutines.sync)で保護した共有Mapとして実装し(`backend-kotlin/src/main/kotlin/com/bffgin/backend/auth/JwksVerifier.kt`参照)、`backend-java`は`ConcurrentHashMap`(java.util.concurrent)を使った。どちらも「共有可変状態をロック/並行対応コレクションで守る」という設計である。このElixir実装(`BackendElixir.Auth.JwksCacheServer`)は対照的に、**ロックという概念そのものを使わない**。GenServerプロセス自身がkid→公開鍵のマップを排他的に所有し、他のプロセス(REST/gRPCハンドラ)は`GenServer.call/2`によるメッセージ送信でのみこの状態にアクセスする。BEAMのプロセスは同時に1つのメッセージしか処理しないため、複数のリクエストが同時にキャッシュへアクセスしようとしても、GenServerのメッセージキューが自然に直列化する。「メモリを共有してロックで守る」のではなく「状態をプロセスに閉じ込め、メッセージでやり取りする」という、アクターモデルの本質的な設計思想の実演になっている。

**Supervisorツリーによる「let it crash」の実演**: `BackendElixir.Application`のトップレベルSupervisor(`:one_for_one`)は、Ecto.Repo・JWKSキャッシュGenServer(local-rsa/keycloakの2issuer分)・REST/gRPCサーバーを配下に置く。JwksCacheServerが構文解析不能なJWKSレスポンスという明確に異常な状況に遭遇した場合、防御的にエラーを握りつぶさず、そのままクラッシュする設計にしている(`JwksCacheServer.crash_with_malformed_response/2`、`test/unit/jwks_cache_server_test.exs`で実際にクラッシュ→Supervisorによる再起動→再起動後も正常に機能することまで確認済み)。これはJava/Kotlin/Pythonの「例外を呼び出し箇所でtry/catchする」設計とは根本的に異なる、OTP固有の障害復旧モデルである。

**BEAMのプリエンプティブなスケジューラ(このプロジェクト全体で最も際立つ技術的事実)**: BEAMの並行処理モデルは「C言語レベルの協調的マルチタスキングの上に成り立つ、Erlang言語レベルのプリエンプティブなマルチタスキング」と説明される。具体的には、BEAMは各プロセスの実行を「reduction」(関数呼び出し・パターンマッチ・メッセージ送信などにほぼ対応する処理単位)でカウントし、**各プロセスに1スケジューリングターンあたり2000reductionの予算を割り当て、使い切ると強制的に他のプロセスへ実行権を譲らせる**。これはI/Oでブロックしているかどうかとは無関係に、CPU計算中のプロセスに対しても等しく適用される。

これは13言語構成の中で唯一の、真にプリエンプティブなスケジューリングである:
- Kotlinのコルーチン: 協調的スケジューリング。`suspend`ポイントで自主的に処理を譲る、譲らないコルーチンは他のコルーチンを飢餓状態にしうる
- Pythonのasyncio: 同じく協調的。`await`ポイントでのみ制御を返す
- Javaの仮想スレッド(Virtual Threads): I/Oブロック時は自動的にキャリアスレッドを解放するが、CPU律速の(I/O待ちを一切挟まない)仮想スレッドはキャリアスレッドを専有し続け、真のプリエンプションは行われない

つまり、**Elixir/BEAMだけが、CPU律速の暴走リクエストが他のリクエストを飢餓状態にすることを言語・VMレベルで防げる**唯一の実装である(参考: [Erlang Scheduler Details and Why It Matters](https://hamidreza-s.github.io/erlang/scheduling/real-time/preemptive/migration/2016/02/09/erlang-scheduler-details.html)、[Deep Diving Into the Erlang Scheduler | AppSignal Blog](https://blog.appsignal.com/2024/04/23/deep-diving-into-the-erlang-scheduler.html))。

**正直な前提**: 本実装のようなステートレスなTask CRUD自体は「リクエストを受けてMySQLに問い合わせて返す」だけの処理で、Erlang/Elixirが本来最も得意とする「大量の同時接続・長時間生存するステートフルなプロセス」という土俵ではない。それでも上記のJWKSキャッシュ(GenServerによるメッセージパッシング)とSupervisor(let it crash)という2点、および後述のFeature Flagポーリング(GenServerによる状態所有の2つ目の実例)は、この規模のアプリケーションでも十分に意味のある実演になっている。

### (e) JWT: 成熟したライブラリ(joken)を使う理由

backend-c/backend-cppはOpenSSLのプリミティブを直接使ってJWT検証を自前実装したが、これは「CやC++にはまともなJWTライブラリが無い」というエコシステムの制約から来た選択であり、普遍的な方針ではない(Go/Rust/JS/TS/Java/Kotlin/Pythonは最初から成熟したライブラリを使っている)。Elixirには`joken`という成熟したライブラリがあるため、同じ判断で素直にライブラリを使う。`Guardian`(より高機能でPhoenix寄りの認証フレームワーク)ではなく`joken`を選んだのは、Phoenixを見送った判断と一貫性を保つためである。学習予算はこのElixir実装固有のテーマ(GenServer・Supervisor・BEAMのスケジューラ)に集中させている。

`Joken.Signer.verify/2`は署名検証のみを行い、exp/iss/aud等のクレーム自体は検証しないため、`HmacVerifier`/`JwksVerifier`が署名検証後にこれらを明示的に確認する(他言語と同じ「ライブラリに全て任せず、境界の検証は自分の目でも確認する」という多層防御の設計)。ヘッダの`alg`が期待するアルゴリズムと完全一致することも、アルゴリズム混同攻撃への対策として独立して確認している。

### (f) 外部公開API・Feature Flagポーリング

**外部公開API(`:8118`、CONTRACT.mdセクション11)**は、`BackendElixir.External.Handler`という内部REST(`Rest.Router`)とは独立した第3の`Plug.Router`+`Plug.Cowboy`リスナーとして実装している。Client Credentials Grant(Keycloak発行のみ)で認証し、内部APIのDispatcherをそのまま再利用しつつ、`BackendElixir.Auth.ExternalAuth`が2点を追加で確認する: (1) issがローカル発行(`bff-gin-local-hmac`/`bff-gin-local-rsa`)でないこと、(2) `claims.azp`が`EXTERNAL_API_CLIENT_ID`と一致すること。認証ロジック自体(署名検証)を複製せず、この2チェックだけを上乗せする設計は、backend-c/backend-cpp/backend-java/backend-kotlin/backend-pythonの`RequireExternalClientAuth`と同じ考え方である。既知の制約として、`user_id`はクライアントが指定した値をそのまま信頼する(JWTの`sub`から解決しない)。

**Feature Flagポーリング**は`BackendElixir.Flags.FeatureFlagPoller`という、`JwksCacheServer`と同じ設計思想のGenServerとして実装した。`feature_flags`テーブルを10秒間隔で`Ecto.Adapters.SQL.query/3`(Ectoの通常のクエリDSLを経由しない、独立したポーリング用の生SQL)で直接ポーリングし、結果を自分だけが排他的に所有する状態としてキャッシュする。`External.Handler`は`GenServer.call/2`(`FeatureFlagPoller.variation/3`)でのみこの状態に問い合わせ、ロックは一切登場しない。JWKSキャッシュがトップレベルSupervisorの下で1issuerにつき1インスタンスだったのに対し、こちらはアプリケーション全体で1つの、常時起動するグローバルなGenServerである点が異なる。

**実機検証で判明した興味深い事実**: このプロジェクトの`backend.external-tasks-pagination-v2`は14言語すべてが同じ1行を共有するグローバルなFeature Flagである。並行して他言語の外部公開API結合テストが同じ行をflip-and-restoreしている状況下でこのフラグを手動でON→11秒待機→確認という素朴な実機検証を行うと、他言語のテストによる書き戻しと競合し、意図した値を読めないことが実際に発生した(結合テスト自身は1プロセス内でflip→即時`FeatureFlagPoller.refresh/1`→assert→restoreを行うため、この競合の影響を受けない)。この経験自体が、GenServerに担当させたポーリング結果キャッシュと、複数の独立したプロセス(今回は言語をまたいだ複数のテストプロセス)が同じ外部リソース(MySQLの1行)を共有する場合の一貫性の限界を、身をもって示す結果になった。

## 認証について

REST/gRPCともに、本物のJWT/JWKS検証(デバッグ用ヘッダは無い)。backend-java/backend-kotlin/backend-pythonと同じ3issuer構成を`lib/backend_elixir/auth/`に実装している:

- **ローカルHMAC**(`iss=bff-gin-local-hmac`): HS256、共有シークレット(`LOCAL_AUTH_HMAC_SECRET`)
- **ローカルRSA**(`iss=bff-gin-local-rsa`): RS256、JWKSはbff自身の`/.well-known/jwks.json`(`LOCAL_AUTH_RSA_JWKS_URL`)から取得
- **Keycloak**: RS256、JWKSはKeycloakの`/protocol/openid-connect/certs`(`KEYCLOAK_ISSUER`から導出、`KEYCLOAK_JWKS_URL`で上書き可)から取得

`Authorization: Bearer <token>`ヘッダ(gRPCは`authorization`メタデータ)を、署名検証前に`iss`だけ覗いて対応するVerifierへ振り分ける`Dispatcher`→実際の署名/`exp`/`aud`検証を行う`HmacVerifier`/`JwksVerifier`という2段構造(他言語と同じ設計)。JWKSベースの2issuer(ローカルRSA・Keycloak)はそれぞれ専用の`JwksCacheServer`(GenServer)を持つ。認証ヘッダが無い、またはいずれの検証にも通らない場合はREST `401 {"error":"unauthenticated"}`・gRPC `UNAUTHENTICATED`。user_id解決(`UserResolver`)はローカル発行issuerなら`sub`をそのまま`users.id`として、Keycloak発行issuerなら`sub`(keycloak_sub)を`user_keycloaks`テーブル経由で引く。

## 結合テスト

実DB(docker-compose上のMySQL)に接続する結合テストは`@moduletag :integration`を付け、`mix test`のデフォルト実行からは除外している(`test/test_helper.exs`の`exclude: [:integration]`参照)。実行するには`mix test --only integration`を使う。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development mix test --only integration
```

### 単体テスト(55件、DB不要、`mix test`)

- `test/unit/hmac_and_dispatcher_test.exs`(10件): HMAC検証(有効/誤った秘密鍵/期限切れ/誤ったaudience/誤ったissuer/意図しないアルゴリズム/`alg:none`)、Dispatcherのissuerルーティング/未知issuer拒否/不正な形式のトークン拒否
- `test/unit/jwks_verifier_test.exs`(7件): 自プロセス内蔵のモックJWKSサーバー(`Plug`+`Plug.Cowboy`を本番と同じライブラリで再利用)+実際に生成したRSA鍵ペアによるRS256検証、未知kid時の再取得、再取得後も未知なkidの拒否、誤ったissuer/audience/期限切れ/意図しないアルゴリズムの拒否
- `test/unit/validation_test.exs`(11件): 名前の20コードポイント制限(基本多言語面外の絵文字含む)、過去日判定、カレンダー上不正な日付の判定、statusのenum判定、ステータスのwire/db値往復
- `test/unit/error_mapper_test.exs`(9件): 各`TaskError`種別→HTTPステータス/JSON形状の対応
- `test/unit/jwks_cache_server_test.exs`(1件): **let it crashの実演**。JwksCacheServerを実際にクラッシュさせ、Supervisorが再起動し、再起動後も正常に機能することを確認する(他言語には無いElixir固有のテストカバレッジ)
- `test/unit/external_query_test.exs`(12件): 外部公開APIのクエリパラメータ解析(`user_id`必須/数値検証、offset/cursorページングのパラメータ境界値の丸め込み)
- `test/unit/external_auth_test.exs`(5件): 外部公開API専用の認証(モックJWKSサーバーでKeycloak相当のトークンを検証)。azp一致で受理、azp不一致/azp欠如で拒否、**正しく署名されたローカルHMACトークンでもissがローカル発行という理由だけで拒否**、認証ヘッダ無しで拒否

### 結合テスト(28件、実DB必須、`mix test --only integration`)

- `test/integration/task_repository_test.exs`(13件): create/find/update/delete一連の往復、存在しないtaskの更新/削除、delete時の`task_labels`孤立行防止、重複label_idの正規化(create/updateの両方)、他ユーザーのtaskへの不可視性、offsetページング(total/limit/offset)、cursorページング(id昇順・after_id)、`find_user_by_id`/`find_user_by_keycloak_sub`
- `test/integration/grpc_service_test.exs`(5件): 実際に起動したgRPCサーバー(テスト専用ポート`:19104`)へ、生成された`Task.V1.TaskService.Stub`から実RPCを送る。CRUD一連の往復、delete時の`task_labels`孤立行防止、認証ヘッダ無しの拒否、期限切れトークンの拒否、cursorページングの連鎖
- `test/integration/external_handler_test.exs`(6件): 実際に起動した外部公開APIリスナー(テスト専用ポート`:18118`)へ、モックJWKSサーバーで実際に署名したRS256トークンを使って実HTTPリクエストを送る。認証ヘッダ無し/azp不一致/ローカルHMACトークン/`user_id`欠如のいずれも拒否、offsetページングがページ境界をまたいで正しく動くこと、cursorページングが`next_cursor`を正しく連鎖させ最終ページで`null`になること(`backend.external-tasks-pagination-v2`は退避→即時`FeatureFlagPoller.refresh/1`→assert→復元し、他言語と共有するこの1行に影響を残さない)
- `test/integration/feature_flag_poller_test.exs`(4件): 実DBに一意なテスト専用flag_keyを自分で挿入・削除して検証する(共有フラグ`backend.external-tasks-pagination-v2`には触れない)。enabled+found→default_variation、disabled→呼び出し側のdefault、未登録→同じくdefault、実DBの値を書き換えてから`refresh/1`した際に実際に反映されること

## セットアップ

```sh
brew install elixir   # Erlang/OTPも依存関係として自動的にインストールされる(初回のみ)
mix deps.get
./scripts/gen_proto.sh   # proto/task/v1/task.protoからElixirコードを生成(lib/generated/、.gitignore対象)
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development HTTP_ADDR=8117 GRPC_ADDR=9104 mix run --no-halt
```

## 動作確認(実機で確認済み)

- REST: 実際に署名したローカルHMAC JWTで`GET/POST/PATCH/DELETE /internal/v1/tasks`のCRUD一連を確認。認証ヘッダ無し・存在しないidへのGETがいずれも正しく`401`/`404`を返すことを確認(後者は下記「実装時に判明した既知の差異」の実バグ修正後に再確認)。生DBの値(`2026-09-12 10:33:15`)とAPIレスポンス(`2026-09-12T10:33:15+00:00`)が完全一致し、タイムゾーンのズレが無いことを確認
- gRPC: `grpcurl -proto proto/task/v1/task.proto`で認証成功時のCRUD・認証ヘッダ無しの`Unauthenticated`をいずれも確認。REST・gRPCとも同一行に対して同一のタイムスタンプが返ることを確認
- create時の重複label_ids([7,7,8])が[7,8]に正規化されること、delete後の再deleteが冪等に`404`を返すことをライブで確認
- 外部公開API: 実際にKeycloakへ`client_credentials`グラントでトークンを取得し、offset(v1、既定)・cursor(v2、`backend.external-tasks-pagination-v2`をON→10秒のポーリング間隔を待つ→反映確認)の両方のページングをライブで確認。生DBの値と一致するタイムスタンプであることも確認。ローカルHMACトークン(内部APIでは正当)が外部公開APIでは正しく`401 unauthenticated`で拒否されることも確認

## ログ

REST v1・外部公開APIともリクエスト単位のログを出す(`BackendElixir.Rest.RequestLogger`という共通のplugを両方の`Plug.Router`に差し込んでいる)。`register_before_send`(Plugが実際にレスポンスを送信する直前のコールバック)で`conn.status`を読むため、どのハンドラが処理したか・途中でエラーになったかに関わらず、実際に送信される最終的なステータスコードを直接記録する

```
rest method=GET path=/internal/v1/tasks status=200 duration_ms=19
external method=GET path=/external/v1/tasks status=401 duration_ms=1
```

gRPC側の`grpc method=... status=... duration_ms=...`(`TaskService.logged/3`)と同じkey=value形式に揃えている

`LOG_LEVEL`(`debug`/`info`/`warn`/`error`、既定`info`。backend(Go)/bff/gateway/goと同じ環境変数名)で詳細度を切り替えられる。`Application.start/2`が起動時に`Logger.configure(level: ...)`でランタイムのログレベルを変更するため、再ビルド無しで有効・無効を切り替えられる。`LOG_LEVEL=debug`にすると、上記の1行サマリに加えて認証で解決した`user_id`・JWKSキャッシュの再取得イベント等の詳細な`Logger.debug`行が追加で出る(Ectoが発行するSQLクエリのdebugログも同時に見えるようになる)

## 実装時に判明した既知の差異(実機検証結果)

- **`find_by_id/2`の戻り値の形が原因の実バグ**: 当初`TaskRepository.find_by_id/2`は見つからない場合に裸のatom`{:error, :not_found}`を返していたが、REST(`Router`)・gRPC(`TaskService`)の各ハンドラは`{:error, %TaskError{}}`という構造体で包まれた形を前提にエラーマッピング(`ErrorMapper.status_and_body/1`・`logged/3`)を行っていたため、`GET /internal/v1/tasks/{存在しないid}`が正しい404ではなく、gRPC側は`CaseClauseError`起因のUNKNOWN(2)、REST側は`FunctionClauseError`という形で顕在化しうる状態だった(gRPC結合テストで実際に再現・修正した)。`find_user_by_id`/`find_user_by_keycloak_sub`は内部の`UserResolver`だけが消費し、そこで明示的に変換しているため元々問題無い。`find_by_id/2`だけをTaskError型で統一して解決した
- **MyXQLは`CLIENT_FOUND_ROWS`をデフォルトで有効にする**: backend-pythonの`aiomysql`/`PyMySQL`はUPDATE文のrowcountが既定で「実際に値が変化した行数」になり(DATETIME列が秒精度のため同一秒内の更新で正当な更新が0件と誤判定されるバグを誘発しうる)、これを避けるため明示的に`client_flag=CLIENT.FOUND_ROWS`を指定する必要があった。MyXQLのソース(`lib/myxql/protocol.ex`)を確認したところ、`:client_found_rows`はクライアント側capability flagsの**基本セットに常時含まれており、明示的な設定は一切不要**である。実機で同一秒内の更新を実際に行い、正しく200(0件不一致による誤404ではない)が返ることを確認した
- **`Ecto.Repo.insert_all/3`の`returning:`オプションはMySQLでは使えない**: PostgreSQLと異なり、MySQLのINSERTはRETURNING句をサポートしない。オートインクリメントidを取得するテストフィクスチャは、`insert_all`ではなく`Ecto.Schema`ベースの`Repo.insert/1`(MySQLのOKパケットが返す`last_insert_id`を内部で利用する)を使う必要がある
- **テストでgRPCクライアントとして接続するには`GRPC.Client.Supervisor`を明示的に起動する必要がある**: 本番はサーバーとしてのみ動作するため`BackendElixir.Application`のSupervisorツリーには含めていないが、結合テストは自分自身にgRPCリクエストを送るクライアントとしても動作するため、`test/integration/grpc_service_test.exs`の`setup_all`で`start_supervised!({GRPC.Client.Supervisor, []})`を明示的に起動している
- **`mix test`実行時はREST/gRPC/外部公開APIリスナーを起動しない**: `BackendElixir.Application`は`Mix.env() == :test`のときPlug.Cowboy(内部・外部)/GRPC.Server.Supervisorの起動をスキップする(Ecto.Repo・JWKSキャッシュGenServer・FeatureFlagPollerは起動する)。常時起動すると複数回のテスト実行やCI環境で固定ポートの衝突を招くため、本番の`main`相当とは別に、結合テストが必要な分だけ明示的にテスト用ポートでサーバーを起動する設計にしている(backend-java/backend-kotlin/backend-pythonのテスト構成と同じ考え方)
- **`FeatureFlagPoller`はデフォルト名で常時1つだけ起動している**: JWKSキャッシュ(issuerごとに複数インスタンス、テストでは無名の別プロセスを都度起動できる)と異なり、`External.Handler`はモジュールのデフォルト名(`BackendElixir.Flags.FeatureFlagPoller`)を素朴に参照する設計にしたため、結合テストからも`start_supervised!`で別名インスタンスを起動するのではなく、`Application.start/2`が既に起動しているグローバルな1インスタンスをそのまま(`refresh/1`等で)操作する必要がある。同名で`start_supervised!`しようとすると`:already_started`エラーになる
- **他言語と共有するFeature Flag行への実機検証は、他言語のテストと競合しうる**: `backend.external-tasks-pagination-v2`は14言語すべてが同じMySQLの1行を共有する。手動でこの行をUPDATEしてから10秒超待って確認する素朴な実機検証は、並行して動いている他言語の結合テスト(同じ行をflip→assert→restoreする)と競合し、意図した値を読めないことが実際にあった。結合テスト自身は1プロセス内でflip→即時`refresh/1`→assert→restoreを行うため、この競合の影響を受けない(上記「アーキテクチャ選定」(f)節参照)
