# backend-haskell

bff-gin: Task CRUD backendのHaskell実装(CONTRACT.mdセクション20の多言語backend比較の14言語目、最後の追加言語)

**このフェーズのスコープ**: 内部REST v1と内部gRPC v2のCRUD、JWT/JWKS認証(ローカルHMAC/ローカルRSA/Keycloakの3issuer)、外部公開API、Feature Flagポーリング。bff/gateway/migrationへの配線はまだ行っていない(下記「未実装・今後の予定」参照)。

## ポート

- 内部REST v1: `:8119`(環境変数`HTTP_ADDR`)
- 内部gRPC v2: `:9105`(環境変数`GRPC_ADDR`)
- 外部公開API: `:8120`(環境変数`EXTERNAL_HTTP_ADDR`)

## アーキテクチャ選定

### (a) Webフレームワーク: Servant(型駆動API設計、Yesodを選ばなかった理由)

2つの案を検討した。

| 案 | 特徴 |
|---|---|
| Yesod | テンプレートHaskell主体のルーティング・型安全なURL・フォーム・永続化(Persistent)まで含む、Haskellの主流フルスタックフレームワーク |
| **Servant(採用)** | APIの形そのものを型レベルで記述する(`:>`/`:<|>`という型演算子でエンドポイントの列を組み立てる)。ルーティング・パラメータのパース・レスポンスの型は全てコンパイル時に決まり、実装(`Server`)がAPI型と一致しなければコンパイルエラーになる |

YesodのようなオピニオネイテッドなフルスタックフレームワークをHaskellでも採用しない理由は、Java実装でSpring Bootを、Kotlin実装でKtorを、Python実装で手書きバリデーション付きFastAPIを、Elixir実装でPhoenixではなくPlug+Cowboyを選んだのと同じ一貫した方針による。「オピニオネイテッドなフレームワーク」という学習教材の役割は、既にRailsが担っている。

Servantを選んだ本質的な理由は「明示的で透明」という他言語共通の基準に加え、Haskell固有の学習価値がある。`type TaskAPI = "internal" :> "v1" :> "tasks" :> Header "Authorization" Text :> ... :> Get '[JSON] Value :<|> ...`という型そのものがAPIドキュメントであり、この型に対応する`Server TaskAPI`の実装が型検査に通らない限りビルドが通らない。エンドポイントを消す/増やす/パスパラメータの型を変えるといった変更は、まず型を変えることから始まり、実装側の追随漏れはコンパイルエラーとして即座に検出される。これはGoの`gin.Engine`やElixirの`Plug.Router`のような「実行時にルートテーブルへ登録する」設計とは対照的な、Haskellならではの型駆動API設計の実演になっている。

### (b) DB: mysql-haskell(生SQL、ORM禁止方針への復帰)

このプロジェクトは13言語(Go/Rust/Scala×2/JS/TS/C++/C/Java/Kotlin/Python)で「生SQL、ORM禁止」を貫いており、Elixir実装だけがEctoという意図的な一回限りの例外だった([backend-elixir/README.md](../backend-elixir/README.md)参照)。Haskell実装では、この方針に戻り、生SQL+手書きの行変換を行う。

**当初計画からの変更点**: 当初は`mysql-simple`(`mysql`パッケージのC言語バインディング経由)を使う予定だったが、依存先の`mysql`パッケージが持つ独自の`Setup.hs`が、GHC 9.14.1にバンドルされた新しいCabalライブラリ(`Distribution.Utils.Path.SymbolicPath`ベースのAPI)と実際にソースレベルで非互換であり、`allow-newer`(バージョン境界の緩和)では解決できない実物の非互換だった。そのため**`mysql-haskell`(純Haskell実装、Cバインディング無し)へ切り替えた**。副作用として、`FromRow`相当の自動導出機構が無いため、`MySQLValue`から`asInt`/`asText`/`asDay`/`asLocalTime`といったヘルパーで素朴に値を取り出す(`TaskRepository.hs`)。これも「ORM禁止・生SQLを直接読み書きする」という方針の一貫した実践と言える。

**もう1つの実装時の発見**: `mysql-haskell`は`CLIENT_FOUND_ROWS`(UPDATE文の`affected_rows`を「マッチした行数」にするcapability flag)を設定する手段を公開していない。backend-pythonはこのflagを明示的に立てることで、DATETIME列が秒精度であるために生じる「同一秒内の更新でaffected_rows=0になり、正当な更新を誤って`not_found`と判定してしまう」バグを回避した。Haskellではこの手段が無いため、`update`/`delete`は`affected_rows`に一切依存せず、独立した`SELECT COUNT(*) ... WHERE id = ? AND user_id = ?`(`rowExists`)で対象行の存在を確認する設計にした。これは実際にバグを踏んでから直したものではなく、`mysql-haskell`のAPIを調査した時点でこの既知の落とし穴を予見し、設計で回避したものである。

### (c) gRPC: grapesy(Well-Typed社の純Haskell実装、恐れていたよりリスクは低かった)

`grapesy`(v1.2.0)は、以前存在した実験的な`grpc-haskell`(gRPC CoreのCバインディング)とは異なり、HTTP/2からgRPCフレーミングまで全て純Haskellで実装された比較的新しいライブラリである。着手前は「新しいライブラリゆえのドキュメント不足・API不安定性」というエコシステムリスクを警戒していたが、実装を通じて次の3点さえ把握すればまったく実用的に使えることが分かった:

1. **型レベルのメソッド列はアルファベット順**: `.proto`の宣言順(`ListTasks, GetTask, CreateTask, UpdateTask, DeleteTask`)ではなく、`proto-lens-protoc`が生成する`ServiceMethods`型インスタンスの並び順(`createTask, deleteTask, getTask, listTasks, updateTask`、アルファベット順)に沿って`RawMethod`の列を組む必要がある
2. **gRPCメタデータ(authorizationヘッダ)へアクセスするには低レベルAPIが必要**: 単純な`Input -> IO Output`の`mkNonStreaming`ではメタデータに触れられないため、`Call`の生の値を受け取る`RawMethod`+`mkRpcHandlerNoDefMetadata`を使う。この場合、ハンドラは最初の出力を送る前に自分で`setResponseInitialMetadata call NoMetadata`を呼ぶ責任を負う(呼び忘れると、認証成功時にのみ`ResponseInitialMetadataNotSet`例外が発生し、認証失敗時は例外経路がTrailers-Onlyにフォールバックするため気づきにくい、という実機検証で踏んだ落とし穴)
3. **`Proto`によるラップ/アンラップ**: `Input`/`Output`型family は低レベルAPIでも`Proto`でラップされた型に解決されるため、`recvFinalInput`の直後に`getProto`で剥がし、`sendFinalOutput`の直前に`Proto`で包む(OverloadedLabelsのフィールドアクセスは剥がした後の素のproto-lens値に対して行う)

`.proto`からのHaskellコード生成は別パッケージ`proto-lens-protoc`(`protoc-gen-haskell`プラグイン)が担う。`google.protobuf.Timestamp`のような共通型は、ローカルでコード生成せず、事前生成済みの`proto-lens-protobuf-types`パッケージをそのまま使う。

### (d) JWT: jose(成熟したライブラリ)

`jose`(Hackageで実績のあるJOSE/JWT実装)を使う。`verifyClaims`は署名検証に加えexp検証も行うが、iss/audの一致確認は他言語と同じ「ライブラリに全てを任せず、境界の検証は自分の目でも確認する」多層防御として、`HmacVerifier`/`JwksVerifier`が署名検証成功後に明示的に行う。ヘッダの`alg`が期待するアルゴリズムと完全一致することも、アルゴリズム混同攻撃対策として署名検証の前に独立して確認している(他言語と同じ設計)。

**実装時に踏んだ落とし穴**: `Crypto.JWT.StringOrURI`の`Show`インスタンスは`deriving Show`によるコンストラクタ表記(`Arbitrary "..."`のような形)になっており、これを`T.pack . show`で生テキストに変換すると期待値と一致しなくなる。正しくは`stringOrUri`プリズムの`review`方向(`StringOrURI -> Text`)を使う。また、`signClaims`はHMAC/RSAの署名生成に`MonadRandom`を要求するが、これは`jose`自身の`JOSE e m`モナド(`newtype JOSE e m a = JOSE (ExceptT e m a)`)にのみ用意されたインスタンスであり、素の`ExceptT JWTError IO`では型検査に失敗する。テストコードの署名ヘルパーは`Crypto.JOSE.runJOSE`を使う必要がある。

### (e) 外部公開API・Feature Flagポーリング

CONTRACT.mdセクション11の外部公開API(`GET /external/v1/tasks`、Client Credentials Grant認証)を、内部REST v1とは独立した3つ目のWarpリスナー(`:8120`)として実装した。`BackendHaskell.External.Api`で2つ目の`ExternalTaskAPI`型を定義し、Servantの型駆動API設計をもう一度小さなエンドポイントで実演している。

- **認可(`RequireExternalClientAuth`相当、`BackendHaskell.Auth.ExternalAuth`)**: 内部トランスポートと同じ`Dispatcher`で署名検証まで行った上で、①issuerがローカル(HMAC/RSA)であれば正しく署名されていても拒否、②`claims.azp`(トークンを取得したOAuth2クライアントのclient_id)が`EXTERNAL_API_CLIENT_ID`(既定`external-api-client`)と一致しなければ拒否、という2段の追加検証を行う。①の検証があるため、内部REST/gRPCで使えるローカルHMAC/RSAトークンは外部公開APIでは一切通用しない
  - **この検証のためだけに`Claims`へ`azp`フィールドを追加した**: Phase 1(内部REST/gRPC)のuser_id解決は`sub`/`iss`のみで足りていたため、`azp`は意図的に省略していた。azpは標準クレームではなくprivate claim(未登録クレーム)のため、joseの`ClaimsSet`には専用のレンズが無く、`unregisteredClaims`(`Data.Map Text Value`)から自分で取り出す(`HmacVerifier.hs`/`JwksVerifier.hs`の`azpOf`)
- **ページング**: `backend.external-tasks-pagination-v2`フラグがOFF(既定)ならoffset(`page`/`page_size`、既定1/10、レスポンス`{"tasks":[...],"page":N,"page_size":N,"total":N}`)、ONならcursor(`cursor`/`limit`、既定なし/10、レスポンス`{"tasks":[...],"next_cursor":"id"|null,"limit":N}`)。既存の`TaskRepository.listOffset`/`listCursor`(gRPC v2のcursorページングと同じ実装)をそのまま再利用し、新規のリポジトリ関数は書いていない。クエリパラメータの解析・デフォルト値・下限クランプは`BackendHaskell.External.Query`という、DB・HTTPを一切介さない純粋関数として分離している(単体テストで直接検証できる)
- **Feature Flagポーリング(STM、JwksCacheに続く2つ目の同種のキャッシュ)**: `BackendHaskell.Flags.FeatureFlagCache`が`feature_flags`テーブルを10秒間隔で`forkIO`のバックグラウンドスレッドからポーリングし、`TVar (Map Text FlagEntry)`へ`atomically`で書き込む。JwksCacheが「未知のkidが来たときだけオンデマンドで再取得」する設計だったのに対し、こちらは無条件の固定間隔ポーリング(bff/他言語のFeature Flagポーリングと同じ戦略)という違いがあるが、「`TVar`+`atomically`だけでロックAPIを一切使わずに共有可変状態を安全に扱う」という設計原理は同じである

### (f) 並行処理モデル: STM(TVar)によるJWKSキャッシュ(4言語比較の最後を締めくくる)

**このHaskell実装で最も学習価値の高い設計判断**であり、Java(`ConcurrentHashMap`、共有可変状態をロック不要のデータ構造で守る)・Kotlin(`Mutex`、明示的にロックを取得・解放する)・Elixir(`GenServer`、状態をプロセスに閉じ込めメッセージでやり取りする)に続く、第4のまったく異なるモデルを提示する。

- **Java**([`backend-java/src/main/java/com/bffgin/backend/auth/JwksVerifier.java`](../backend-java/src/main/java/com/bffgin/backend/auth/JwksVerifier.java)): `ConcurrentHashMap`という「ロックなしで並行安全な可変コレクション」に直接書き込む
- **Kotlin**([`backend-kotlin/src/main/kotlin/com/bffgin/backend/auth/JwksVerifier.kt`](../backend-kotlin/src/main/kotlin/com/bffgin/backend/auth/JwksVerifier.kt)): `kotlinx.coroutines.sync.Mutex`を明示的に`lock`/`unlock`し、共有Mapを保護する
- **Elixir**([`backend-elixir/lib/backend_elixir/auth/jwks_cache_server.ex`](../backend-elixir/lib/backend_elixir/auth/jwks_cache_server.ex)): 状態そのものをGenServerプロセスに閉じ込め、他プロセスはメッセージ送信(`GenServer.call/2`)でのみアクセスする。ロックという概念自体が存在しない
- **Haskell**(`src/BackendHaskell/Auth/JwksCache.hs`、本実装): `STM.TVar`に状態を持ち、読み書きは`atomically`ブロックの中で行う

STMは上記のどれとも異なる第5の(実質的には第4のカテゴリの)アプローチである。ロックを明示的に取得・解放するAPIは一切登場しない。`atomically`ブロックはトランザクションとして実行され、実行中に他のトランザクションと競合(同じ`TVar`への書き込みが割り込む等)したことを検出すると、コミットせずに**自動的にリトライ**する。これは「悲観的ロック」ではなく「楽観的並行性制御」であり、かつ`STM a`という型そのものが「これはSTMトランザクションの中でしか実行できない」ことを型システムで強制する(`IO`アクションを`atomically`の中で誤って実行することはコンパイルエラーになる)。`test/Unit/JwksCacheSpec.hs`の「20個の`forkIO`スレッドが同時に`refresh`/`lookupKey`を行っても状態が破損しない」テストは、この実演になっている。

## IOモナドについて(backend-scala-http4sとの対比)

このプロジェクトの実装言語のうち、Haskellとbackend-scala-http4s(cats-effect)だけが「副作用を型で明示的に表現する」という共通の設計思想を持つ。ここでは何が同じで、何が違うかを明確にしておく。

**同じ点**: 両方とも、副作用(DBへのI/O、HTTPでのJWKS取得など)を行う関数の**戻り値の型に必ず`IO`が現れる**。`backend-haskell/src/BackendHaskell/Repository/TaskRepository.hs`の全公開関数(`listOffset`, `findById`, `create`, `update`, `delete`等)は`IO [...]`を返し、[backend-scala-http4s/src/main/scala/com/bffgin/backend/TaskRepo.scala](../backend-scala-http4s/src/main/scala/com/bffgin/backend/TaskRepo.scala)の対応するメソッド(`listOffset`, `get`, `create`, `update`, `delete`等)は`cats.effect.IO[...]`を返す。どちらも「副作用を伴う計算を、実行せずに値として組み立て、呼び出し側が最後に一度だけ実行する」という設計を型で強制しており、`TaskRepo.scala`にもこの対比を説明する短いコメントを追加してある(相互参照)。

**違う点(重要)**:

1. **言語組み込みのRTSプリミティブか、サードパーティのライブラリか**: Haskellの`IO`は言語のランタイムシステム(RTS)に組み込まれたプリミティブ型であり、**全ての副作用がこの型を経由する唯一の道**である。`IO`を経由しない限り、Haskellのコードから副作用のあるコードを呼び出すこと自体ができない(型システムによる強制がコンパイラの実装そのものに根ざしている)。一方cats-effectの`IO`はScalaという言語自体には組み込まれていない、サードパーティのライブラリが提供するデータ型である。Scalaの通常のメソッドは、`IO`を経由せずに直接副作用を起こすことが言語的には可能であり、「副作用を`IO`に閉じ込める」という規律はライブラリとチームの規約によって保たれている。
2. **既定の評価戦略が遅延か正格か**: Haskellは既定で遅延評価であり、`IO`アクションも「実行されるまで何も起こらない値」として組み立てられる。これはHaskellの評価戦略そのものの自然な延長である。cats-effectの`IO`も同様に「実行されるまで何も起こらない」性質(参照透明性)を持つが、これはScala(既定で正格評価の言語)の上に、ライブラリが独自に実現している性質である。

まとめると、両者は「副作用を型で表現し、実行を遅らせて合成する」という設計思想では一致するが、Haskellではこれが言語仕様そのものであるのに対し、Scala/cats-effectではこれがライブラリによって注意深く構築された規律である、という違いがある。

## 認証について

REST/gRPCともに、本物のJWT/JWKS検証(デバッグ用ヘッダは無い)。backend-java/backend-kotlin/backend-python/backend-elixirと同じ3issuer構成を`src/BackendHaskell/Auth/`に実装している:

- **ローカルHMAC**(`iss=bff-gin-local-hmac`): HS256、共有シークレット(`LOCAL_AUTH_HMAC_SECRET`)
- **ローカルRSA**(`iss=bff-gin-local-rsa`): RS256、JWKSはbff自身の`/.well-known/jwks.json`(`LOCAL_AUTH_RSA_JWKS_URL`)から取得
- **Keycloak**: RS256、JWKSはKeycloakの`/protocol/openid-connect/certs`(`KEYCLOAK_ISSUER`から導出、`KEYCLOAK_JWKS_URL`で上書き可)から取得

`Authorization: Bearer <token>`ヘッダ(gRPCは`authorization`メタデータ)を、署名検証前に`iss`だけ覗いて対応するVerifierへ振り分ける`Dispatcher`→実際の署名/`exp`/`aud`検証を行う`HmacVerifier`/`JwksVerifier`という2段構造(他言語と同じ設計)。JWKSベースの2issuer(ローカルRSA・Keycloak)はそれぞれ専用の`JwksCache`(STM)を持つ。認証ヘッダが無い、またはいずれの検証にも通らない場合はREST `401 {"error":"unauthorized"}`・gRPC `UNAUTHENTICATED`。user_id解決(`UserResolver`)はローカル発行issuerなら`sub`をそのまま`users.id`として、Keycloak発行issuerなら`sub`(keycloak_sub)を`user_keycloaks`テーブル経由で引く。

gRPCクライアントが`authorization`メタデータを送るには、grapesyの`CallParams`の`callRequestMetadata`フィールド(型family`RequestMetadata`で決まる型)を使う必要がある。`RequestMetadata (Protobuf TaskService meth)`は`grpc-spec`があらかじめ用意している`[CustomMetadata]`型(生のヘッダリストそのまま)を指定しており、サーバー側は`getRequestHeaders`経由の低レベルAPIで同じ生メタデータを読む(`src/Proto/API/Task/V1/Task.hs`参照)。

## 結合テスト

実DB(docker-compose上のMySQL)に接続する結合テストは別の`test-suite`(`backend-haskell-integration`)に分けており、`cabal test test:backend-haskell-unit`のデフォルト実行には含まれない。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
cabal test test:backend-haskell-integration --test-show-details=direct
```

### 単体テスト(62件、DB不要、`cabal test test:backend-haskell-unit`)

- `test/Unit/HmacAndDispatcherSpec.hs`(9件): `isLocalIssuer`の判定、HMAC検証(有効/誤った秘密鍵/期限切れ/誤ったaudience/誤ったissuer/`alg:none`拒否)、Dispatcherのissuerルーティング/未知issuer拒否/不正な形式のトークン拒否
- `test/Unit/JwksVerifierSpec.hs`(7件): 自プロセス内蔵のモックJWKSサーバー(`warp`を本番と同じライブラリで再利用)+実際に生成したRSA鍵ペアによるRS256検証、未知kid時の再取得、再取得後も未知なkidの拒否、誤ったissuer/audience/期限切れ/意図しないアルゴリズムの拒否
- `test/Unit/JwksCacheSpec.hs`(3件): refresh前のlookupがNothingを返す、refreshでキャッシュが埋まる、**20並行の`forkIO`スレッドが同時にrefresh/lookupを行っても状態が破損しない(STMの並行安全性の実演、他言語には無いHaskell固有のテストカバレッジ)**
- `test/Unit/ValidationSpec.hs`(11件): 名前の20コードポイント制限(基本多言語面外の絵文字含む)、過去日判定、カレンダー上不正な日付の判定、statusのenum判定
- `test/Unit/ErrorMapperSpec.hs`(9件): 各`TaskError`種別→HTTPステータス/JSON形状の対応
- `test/Unit/ExternalQuerySpec.hs`(18件): 外部公開APIのクエリパラメータ解析(`user_id`必須判定、`page`/`page_size`/`limit`の下限クランプ、`cursor`の欠落/非数値時のフォールバック)
- `test/Unit/ExternalAuthSpec.hs`(5件): `RequireExternalClientAuth`相当の検証(Keycloak発行+azp一致→受理、azp欠落/不一致→拒否、**正しく署名されたローカルHMACトークンでもissuerがローカルという理由だけで拒否**、認証ヘッダ無し→拒否)

### 結合テスト(27件、実DB必須、`cabal test test:backend-haskell-integration`)

- `test/Integration/TaskRepositorySpec.hs`(13件): create/find/update/delete一連の往復、存在しないtaskの更新/削除、delete時の`task_labels`孤立行防止、重複label_idの正規化(create/updateの両方)、他ユーザーのtaskへの不可視性、offsetページング(total/limit/offset)、cursorページング(id昇順・after_id)、`find_user_by_id`/`find_user_by_keycloak_sub`
- `test/Integration/GrpcServiceSpec.hs`(5件): 実際に起動したgrapesyサーバー(テスト専用ポート`:19107`)へ、grapesyクライアントの生API(`withRPC`/`sendFinalInput`/`recvFinalOutput`)で実RPCを送る。CRUD一連の往復、delete時の`task_labels`孤立行防止、認証ヘッダ無しの拒否、期限切れトークンの拒否、cursorページングの連鎖
- `test/Integration/ExternalHandlerSpec.hs`(6件): 実際に起動したWarpサーバー(外部公開API専用、テスト用ポート`:18120`)+自プロセス内蔵モックJWKSサーバー(`:19120`、Keycloak相当)へ、`http-client`で実HTTPリクエストを送る。認証ヘッダ無し/azp不一致/ローカルHMAC/user_id欠如の拒否、offsetページングのページ境界、cursorページングが`next_cursor: null`まで連鎖することを確認。`backend.external-tasks-pagination-v2`は退避→書き換え→`finally`で確実に復元する(全言語で共有する1つのフラグのため)
- `test/Integration/FeatureFlagCacheSpec.hs`(3件): 実DBに一意なテスト専用`flag_key`行を挿入し、`variation`のフォールバック規則(enabled+found→default_variation、disabled→default_value、not-found→default_value)を検証。既存の共有flag行には一切触れない

## ログについて

REST v1・外部公開API・gRPCともリクエスト単位のログを標準出力へ出す(いずれも`method`/`status`/`duration_ms`を含むkey=value形式)。

- REST: `rest method=<HTTPメソッド> path=<パス> status=<実際のHTTPステータス> duration_ms=<処理時間>`(`src/BackendHaskell/Logging.hs`の`requestLoggingMiddleware`、WAIの`Middleware`としてServantが生成する`Application`を包む)
- 外部公開API: 同じ形式で接頭辞のみ`external`
- gRPC: `grpc method=<RPC名> status=<gRPCステータス> duration_ms=<処理時間>`(`src/BackendHaskell/Grpc/TaskService.hs`の`logged`、Phase 1から実装済み)

**`LOG_LEVEL`(既定`info`)**: `backend`(Go)・bff・gateway/goと同じ環境変数名。`LOG_LEVEL=debug`にすると、上記の常時出力される1行に加えて、認証で解決した`user_id`・RESTのクエリパラメータ・JWKSキャッシュの再取得タイミングなど、より詳細な`[debug] <文脈>: <メッセージ>`形式の行が追加で出力される(`BackendHaskell.Logging.logDebug`)。既定の`info`ではこれらは出力されない。

【実機検証で見つかった実バグ】stdoutをファイル/パイプへリダイレクトすると、GHCランタイムの既定(ブロックバッファリング)ではプロセス終了までログが一切flushされない。`Main.hs`冒頭で`hSetBuffering stdout LineBuffering`を明示することで解決した(本番相当の運用でstdoutをログ収集基盤へリダイレクトする場合に必須の対応)。

## セットアップ

```sh
# 初回のみ
brew install ghc cabal-install pcre snappy
# 初回のみ
cabal build --only-dependencies
# 初回・proto変更時のみ
mkdir -p generated && protoc --plugin=protoc-gen-haskell=$(cabal list-bin proto-lens-protoc) \
  --haskell_out=generated -I proto -I /opt/homebrew/include proto/task/v1/task.proto
cabal build
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development \
  HTTP_ADDR=8119 GRPC_ADDR=9105 EXTERNAL_HTTP_ADDR=8120 cabal run exe:backend-haskell-server
```

### `generated/` と `src/Proto/API` の使い分け(重要)

| パス | 作り方 | Git管理 |
|---|---|---|
| `generated/Proto/Task/V1/Task.hs`・`Task_Fields.hs` | `protoc`+`proto-lens-protoc`で自動生成 | しない(`.gitignore`対象) |
| `src/Proto/API/Task/V1/Task.hs` | **手書き**(grapesy用のメタデータ型family宣言+生成モジュールの再エクスポート) | する |

`proto-lens-protoc`はメッセージ/サービスの型しか生成せず、grapesy が gRPC サービスとして扱うための
`RequestMetadata`/`ResponseInitialMetadata`/`ResponseTrailingMetadata`の宣言は手書きする必要がある
(grapesyの`tutorials/quickstart/src/Proto/API/Helloworld.hs`と同じパターン)

**手書きファイルを`generated/`に置かないこと。** 以前は`generated/`に置いていたため`.gitignore`で除外されて
一度もコミットされず、クリーンな環境では `can't find source for Proto/API/Task/V1/Task` でビルドできない状態になっていた(2026-09-25に判明し、`src/`へ移動して修正)
`hs-source-dirs: src, generated` のため、`src/` に置けばcabalの設定変更なしでビルドできる

`cabal list-bin proto-lens-protoc`が解決できない環境では、
`cabal install proto-lens-protoc --installdir=<任意のディレクトリ>`で入手したバイナリを
`--plugin=protoc-gen-haskell=<パス>`へ指定する


## 動作確認(実機で確認済み)

- REST: 実際に署名したローカルHMAC JWTで`GET /internal/v1/tasks/{id}`を実行し、200/401(認証ヘッダ無し・期限切れ)/404(存在しないid)がいずれも正しく返ることを確認。生DBの値(`2026-09-12 10:33:15`)とAPIレスポンス(`2026-09-12T10:33:15+00:00`)が完全一致し、タイムゾーンのズレが無いことを確認
- gRPC: `grpcurl -proto proto/task/v1/task.proto`で認証成功時の`GetTask`/`ListTasks`・認証ヘッダ無しの`Unauthenticated`をいずれも確認。同一行(id=383)に対してREST・gRPCとも同一のタイムスタンプ(`2026-09-12T10:33:15Z`)が返ることを確認
- 外部公開API: 実際に起動中のKeycloakへ`grant_type=client_credentials`(`client_id=external-api-client`)でトークンを取得し、`GET /external/v1/tasks`のoffsetページング(200、内部RESTと同一のタイムスタンプ)・`backend.external-tasks-pagination-v2`をONにした状態でのcursorページング(200、`next_cursor`が正しく連鎖)をいずれも確認。正しく署名されたローカルHMAC JWT(`azp`が一致していても)は401で拒否されることも確認。検証後、`backend.external-tasks-pagination-v2`は元の状態(`enabled=0, default_variation='off'`)に復元済み
- ログ: REST・外部公開APIの実リクエスト(200/401)に対し、`rest method=... status=... duration_ms=...`・`external method=... status=... duration_ms=...`が実際のステータスコードの値で標準出力に出ることを確認。`LOG_LEVEL=debug`起動時のみ`[debug] auth: resolved user_id=...`等の追加行が出て、既定(`info`)では出ないことも確認

## 未実装・今後の予定

- bff/gateway/migrationへの配線(`backend.task-language`への`haskell`の追加)
