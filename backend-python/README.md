# backend-python

bff-gin: Task CRUD backendのPython実装(CONTRACT.mdセクション20の多言語backend比較の12言語目)

**このフェーズのスコープ**: 内部REST v1・内部gRPC v2のCRUD、JWT/JWKS認証(ローカルHMAC/ローカルRSA/Keycloakの3issuer)、外部公開API、Feature Flagポーリング。bff/gateway/migrationへの配線はまだ行っていない(下記「未実装・今後の予定」参照)。

## ポート

- 内部REST v1: `:8115`(環境変数`HTTP_ADDR`。他言語と同じくポート番号のみを受け付ける簡略形式)
- 内部gRPC v2: `:9103`(環境変数`GRPC_ADDR`、同じくポート番号のみ)
- 外部公開API: `:8116`(環境変数`EXTERNAL_HTTP_ADDR`)

## アーキテクチャ選定

### (a) HTTPフレームワークにFastAPIを選んだ理由

3つの案を検討した。

| 案 | 特徴 |
|---|---|
| **FastAPI(採用)** | Starletteベースの非同期ネイティブフレームワーク。Pydanticによる型ヒントベースの構造的なリクエスト解析、`async def`ハンドラ |
| Flask | 同期(WSGI)前提の伝統的な軽量フレームワーク。2.0+で`async def`ハンドラを書けるが、内部的にasgirefでイベントループへブリッジしているだけで、ネイティブな非同期ではない |
| Starlette(FastAPIの下地)を直接使う | 最小限のASGIフレームワーク、自動検証無し、より明示的 |

Flaskを選ばなかった理由は、backend-kotlinで「Javalinの流用」を見送ったのと同じ構造の判断である。Python実装の狙いは「GILとasyncioという非同期モデルを実際に体験すること」であり、WSGI前提の設計に後付けで非同期を接続したフレームワークでは、この狙いが達成できない。

**FastAPIのPydantic自動検証について**: このプロジェクトはSpring Boot(Java実装で不採用)のようなDI/ORM自動化を一貫して避けてきた。FastAPIのPydanticも一見「魔法」に見えるが、性質が異なる。Pydanticの役割は「JSON文字列→型付きオブジェクト」という**構造的な型変換**に限定しており、Go/Rust/Java/KotlinがJSONをstruct/data classへデシリアライズする際に行う型変換と本質的に同じである。**ビジネスルールの検証(name必須・20コードポイント制限・finished_onの過去日判定・statusのenum判定)はPydanticのバリデータに一切任せず、`app/domain/validation.py`に他言語の`TaskValidation`相当として明示的に手書きしている**(このプロジェクト全言語共通の方針、`app/rest/routes.py`の`TaskRequestBody`のdocstring参照)。

### (b) gRPC: grpcio + grpc.aio

`grpcio`は公式・成熟したライブラリで、同期API(`grpc`)と非同期API(`grpc.aio`)の両方を提供する。FastAPIのasyncioイベントループと統一するため`grpc.aio`(非同期サービサー)を採用している。

**Java/Kotlinとの対比**: grpc-java/grpc-kotlinは、認証ヘッダをコルーチン/スレッドを跨いで伝播するために専用の`ServerInterceptor`(`io.grpc.Context`経由)を必要とした。`grpc.aio`の非同期サービサーメソッドは`context.invocation_metadata()`で直接メタデータへアクセスできるため、この種のブリッジ用インターセプタは不要で、各RPCメソッドから直接読み取ればよい(`app/grpc_/service.py`の`_authenticate`参照)。

### (c) DB: aiomysql(ORM禁止)、3言語目の「ブロッキングI/O隔離」比較

生SQL + `aiomysql`のみ(ORM禁止方針、他言語と統一。SQLAlchemy/Tortoise ORM等は使わない)。

**このPython実装で最も学習価値の高い設計判断、backend-java/Main.javaのstartRestServer()・backend-kotlin/TaskRepository.ktのクラスコメントと対になる、3言語目の比較**:

- backend-java(Virtual Threads)は「並行処理の安全性を自動化する」設計だった。JDBCのような普通のブロッキング呼び出しをそのまま書いても、JVMが「仮想スレッドがI/Oでブロックする瞬間」を自動的に検知し、少数のOSキャリアスレッドを他の仮想スレッドへ譲ってくれる。呼び出し側には特別な記述が一切不要だった
- backend-kotlin(`Dispatchers.IO`)は対照的に、「並行処理の安全性を型システムと明示的なディスパッチャ選択で保証する」設計だった。JDBCには「本質的に非同期なDB接続」という概念自体が存在しないため、呼び出し側が`withContext(Dispatchers.IO)`で「これはブロッキングI/Oである」と自己申告する必要があった
- **このPython実装は第3の解決策を示す**: `aiomysql`は最初から非同期ネイティブなドライバであり、`await cursor.execute(...)`は実際のソケットI/O待ちで自然にイベントループへ制御を返す。Pythonのasyncioエコシステムには`aiomysql`/`asyncmy`のようなドライバレベルで真に非同期なMySQLクライアントが存在するため、`app/repository/task_repository.py`のどのメソッドにも「これはブロッキングI/Oである」と自己申告するためのコードは一切登場しない

つまり「非同期ランタイムからブロッキングI/Oをどう隔離するか」という同じ主題に対し、C++(自作`asio::thread_pool`で手動隔離)→Kotlin(標準ライブラリ提供の`Dispatchers.IO`で明示的に隔離)→Python(ドライバ自体が非同期ネイティブなので隔離が不要)という3段階の解決策が、13言語構成の中で揃うことになる。

### (d) GILと「後付けの非同期」という歴史的経緯

- **GIL(Global Interpreter Lock)**: CPythonのバイトコードを実行できるスレッドは常に1つだけである。asyncioを使ってもこれは変わらないが、I/O待ち中はGILが解放されるため、今回のようなI/O律速のCRUDアプリケーションでは実害が無い。CPU律速の処理(暗号計算・画像処理等)は、Pythonのスレッドを増やしても並列化されない(マルチプロセスが必要になる)
- **後付けの非同期という経緯**: Pythonは元々WSGI(同期、スレッド/プロセスベース)を前提に設計され、`asyncio`(3.4+)・`async`/`await`構文(3.5+)は後から追加された。この結果、`requests`→`httpx`/`aiohttp`、`PyMySQL`→`aiomysql`のように、同期版と非同期版のライブラリが分裂するエコシステムが生まれた。これはGo/Node.js(最初から非同期前提)やKotlin(言語機能として比較的早期からコルーチンを持つ)とは対照的な、Python固有の歴史的制約である

### (e) JWT: 成熟したライブラリ(PyJWT)を使う理由

backend-c/backend-cppはOpenSSLのプリミティブを直接使ってJWT検証を自前実装したが、これは「CやC++にはまともなJWTライブラリが無い」というエコシステムの制約から来た選択であり、普遍的な方針ではない(Go/Rust/JS/TS/Java/Kotlinは最初から成熟したライブラリを使っている)。Pythonには`PyJWT`という成熟したライブラリがあるため、同じ判断で素直にライブラリを使う。学習予算は代わりにGIL/asyncio/ドライバレベル非同期という、このPython実装固有のテーマに集中させている。

`PyJWT`の`jwt.decode(algorithms=[...])`は、指定したアルゴリズム以外(`none`含む)のトークンをそもそも受理しない設計になっているが、アルゴリズム混同攻撃への多層防御として、ヘッダの`alg`が期待するアルゴリズム(HmacVerifierならHS256、JwksVerifierならRS256)と完全一致することも明示的に確認している(`app/auth/hmac_verifier.py`・`app/auth/jwks_verifier.py`参照)。

### (f) 外部公開API・Feature Flagポーリング

`GET /external/v1/tasks`(CONTRACT.mdセクション11)。認証は内部REST/gRPCと同じ`Dispatcher`を再利用しつつ、`app/auth/external_auth.py`の`require_external_client()`が2段の追加チェックを行う: (1) issがローカル発行(HMAC/RSA)であれば拒否(Keycloak発行のClient Credentials Grantトークンのみ許可)、(2) `azp`(authorized party)クレームが`EXTERNAL_API_CLIENT_ID`と一致しなければ拒否。ローカルHMACトークンは署名検証自体が正しく通り、かつ`azp`が一致していても、issがローカル発行である時点で拒否される(このチェックで最も間違えやすい振る舞い、`tests/unit/test_external_auth.py`/`tests/integration/test_external_handler.py`で個別に確認済み)。

ページングは`backend.external-tasks-pagination-v2`フラグで切り替える(既定OFF): OFFはoffset方式(`page`/`page_size`/`total`)、ONはcursor方式(`cursor`/`limit`/`next_cursor`、cursorの実体は直前ページ最後のtaskの`id`)。内部REST v1の`list_offset`・内部gRPC v2の`list_cursor`をそのまま再利用しており、新規のRepositoryメソッドは追加していない。

Feature Flagポーリング(`app/flags/feature_flag_poller.py`)は`feature_flags`テーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、他言語と同じ設計)。**このPython実装ならではの単純さ**: `aiomysql`は他の全DBアクセスと同じ非同期ネイティブドライバなので、ポーリング専用の隔離やロックは一切不要で、既存の共有プールをそのまま使い回すだけでよい。キャッシュの更新はモジュールレベルの辞書を(mutateせず)丸ごと置き換えることで行う。CPythonのGILにより単一の代入操作は他のコルーチンから見て中断されないため、読み取り側もロックを取らずに安全に読める(「明示的な隔離が不要」という、このPython実装の一貫したテーマのもう一つの実演)。

## 認証について

REST/gRPCともに、本物のJWT/JWKS検証。backend-java(`Config.kt`)・backend-kotlin・backend-rust(`src/auth/mod.rs`)と同じ3issuer構成を`app/auth/`に実装している:

- **ローカルHMAC**(`iss=bff-gin-local-hmac`): HS256、共有シークレット(`LOCAL_AUTH_HMAC_SECRET`)
- **ローカルRSA**(`iss=bff-gin-local-rsa`): RS256、JWKSはbff自身の`/.well-known/jwks.json`(`LOCAL_AUTH_RSA_JWKS_URL`)から取得
- **Keycloak**: RS256、JWKSはKeycloakの`/protocol/openid-connect/certs`(`KEYCLOAK_ISSUER`から導出、`KEYCLOAK_JWKS_URL`で上書き可)から取得

`Authorization: Bearer <token>`ヘッダ(gRPCは`authorization`メタデータ)を、署名検証前に`iss`だけ覗いて対応するVerifierへ振り分ける`Dispatcher`(`app/auth/dispatcher.py`)→実際の署名/`exp`/`aud`検証を行う`HmacVerifier`/`JwksVerifier`という2段構造(他言語と同じ設計)。認証ヘッダが無い、またはいずれの検証にも通らない場合はREST `401 {"error":"unauthorized"}`・gRPC `UNAUTHENTICATED`。user_id解決(`app/auth/user_resolver.py`)はローカル発行issuerなら`sub`をそのまま`users.id`として、Keycloak発行issuerなら`sub`(keycloak_sub)を`user_keycloaks`テーブル経由で引く。

JWKSはkid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけ再取得する「kid不一致時のみ再取得」戦略(`app/auth/jwks_verifier.py`、他言語と同じ)。再取得の排他制御には`asyncio.Lock`を使う(`threading.Lock`はイベントループをブロックするため誤り、backend-kotlinの`Mutex`/`withLock`と同じ設計判断)。JWKS取得はhttpxの非同期クライアントで行う(同期の`requests`をasync関数内で呼ぶと、その1箇所だけイベントループをブロックしてしまうため)。

## 結合テスト

実DB(docker-compose上のMySQL)に接続する結合テストは、`pytest`のデフォルト対象には含めない(`@pytest.mark.integration`でマークし、`pyproject.toml`の`addopts`で既定除外。`pytest`実行のたびにdocker composeを要求しないため)。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
source .venv/bin/activate
pip install -r requirements-dev.txt
pytest                                    # 単体テストのみ(DB不要)
pytest -m integration                     # 結合テストのみ(実DB必須)
```

### 単体テスト(54件、DB不要)

- `tests/unit/test_hmac_and_dispatcher.py`(10件): HmacVerifierの正常系・不正な秘密鍵/期限切れ/audience不一致/issuer不一致/アルゴリズム不一致/`alg:none`拒否、Dispatcherのissuerルーティング・未知issuer拒否・不正な形式のトークン拒否
- `tests/unit/test_jwks_verifier.py`(7件): 自プロセス内にモックJWKSサーバー(`tests/unit/support/mock_jwks_server.py`、標準ライブラリの`http.server`)を立て、実際のRSA鍵ペアで署名したRS256トークンの検証・未知kidでの再取得・issuer/audience/期限切れ/アルゴリズム不一致の拒否を確認
- `tests/unit/test_validation.py`(10件): バリデーションの境界値(空文字・20コードポイントちょうど・21コードポイント・絵文字を含む名前の境界値・過去日・当日・不明なstatus)
- `tests/unit/test_error_mapper.py`(9件): `TaskError`の全9種類がJSON形状・HTTPステータスへ正しくマッピングされること
- `tests/unit/test_external_query.py`(12件): 外部公開APIのクエリパラメータ解析(`user_id`必須/非数値拒否、offset方式の`page`/`page_size`の既定値・下限クランプ・非数値時の既定値フォールバック、cursor方式の`cursor`/`limit`の同様の扱い)
- `tests/unit/test_external_auth.py`(6件): `require_external_client()`が実RSA鍵+モックJWKSサーバーでKeycloak発行トークン(`azp`一致)を受理、`azp`不一致/欠如を拒否、**ローカルHMACトークンは署名検証が正しく通り`azp`が一致していても拒否**、認証ヘッダ無し/不正な形式を拒否

### 結合テスト(29件、実DB必須)

- `tests/integration/test_task_repository.py`(13件): create/find/update/delete一連の往復、delete時の`task_labels`孤立行防止(トランザクション保護)、重複label_idの正規化(create/update両方)、他ユーザーのtaskへの不可視性、offsetページングの`total`/件数、cursorページングの順序、`find_user_by_id`/`find_user_by_keycloak_sub`
- `tests/integration/test_grpc_service.py`(5件): 実gRPCサーバー(`grpc.aio.server()`)を起動し、生成された`TaskServiceStub`で実際にRPCを送る。CRUD一連の往復、delete時の`task_labels`孤立行防止、`authorization`メタデータ無し/期限切れトークンでの`UNAUTHENTICATED`、cursorページングの連鎖
- `tests/integration/test_external_handler.py`(7件): 実DB+FastAPIの外部公開APIルーターを`httpx.ASGITransport`で直接叩く。認証ヘッダ無し/ローカルHMAC拒否(azp一致でも)/azp不一致/`user_id`欠如・非数値の拒否、offsetページングのページ境界、cursorページングが`next_cursor`を正しく連鎖させ最終ページで`null`になること(検証中は共有の`backend.external-tasks-pagination-v2`フラグを一時的にONにし、`finally`で必ずOFFへ復元する)
- `tests/integration/test_feature_flag_poller.py`(4件): `Variation()`のフォールバック規則(enabled+存在→default_variation、disabled→呼び出し側のdefault_value、flag_key不明→同じくdefault_value)、`start()`/`stop()`によるバックグラウンドタスクのライフサイクル。他機能が参照する既存のflag行には一切触れない、テスト専用の使い捨て`flag_key`を使う

## セットアップ

```sh
brew install python@3.12
python3.12 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

# .protoからのコード生成(初回・proto変更時のみ)
python -m grpc_tools.protoc \
  -I proto \
  -I "$(python3 -c 'import grpc_tools, os; print(os.path.join(os.path.dirname(grpc_tools.__file__), "_proto"))')" \
  --python_out=app/generated --grpc_python_out=app/generated --pyi_out=app/generated \
  proto/task/v1/task.proto

python -m app.main
```

## ログについて

`LOG_LEVEL`環境変数("debug"/"info"/"warn"/"error"、既定`info`、backend(Go)/bff/gateway(Go)と同じ規約)でログの詳細度を切り替えられる。標準ライブラリの`logging`モジュールのみを使う(追加の依存ライブラリは入れない方針)

- **リクエスト単位のログ(INFO、既定で常に出る)**: 内部REST v1・外部公開APIともに、`app/logging_middleware.py`の`@app.middleware("http")`が全ルートへ横断的に適用され、`rest method=... path=... status=... duration_ms=...`/`external method=... path=... status=... duration_ms=...`という、既存のgRPCログ(`grpc method=... status=... duration_ms=...`、`app/grpc_/service.py`)と同じkey=value形式で1行出す。ハンドラ個別に埋め込むのではなくミドルウェア1箇所に実装することで、ルート追加時に書き漏れる心配が無い
- **DEBUG時のみ出る追加ログ**: `LOG_LEVEL=debug`で起動すると、上記の要約行に加えて認証・クエリ解析の内部詳細(解決した`user_id`、`JwksVerifier`のkid不一致によるJWKS再取得、REST/外部APIで解析したページングパラメータ)が`log.debug(...)`で出る
- Pythonの`logging`の既知の落とし穴として、`logging.basicConfig()`はroot loggerに既にハンドラが付いていると2回目以降は何もしない。このため`configure_logging()`(`app/main.py`)は環境変数を読み終えた直後、他の何よりも先に一度だけ呼ぶ設計にしている

## 動作確認(実機で確認済み)

- REST: 認証ヘッダ無し→`401`、実際に署名したローカルHMAC JWTでの`GET /internal/v1/tasks`が既存データを正しいJSON形状で返す(`created_at`/`updated_at`が生DBの値と完全一致することを直接SQLで突き合わせ確認済み)、期限切れJWTでの呼び出し→`401`
- リクエスト単位のログ: 上記REST/外部公開APIの`200`/`401`/`404`いずれの呼び出しでも、標準出力に`rest method=GET path=/internal/v1/tasks status=200 duration_ms=6`のような行が実際に出ることを確認済み。`LOG_LEVEL=debug`で起動すると、この要約行に加えて`resolved user_id=1 via local issuer=...`等のDEBUG行も出ることを確認済み(既定の`info`ではDEBUG行が出ないことも確認済み)
- gRPC: `grpcurl`で実際に署名したローカルHMAC JWTを`authorization`メタデータに付けた`ListTasks`が正しいレスポンスを返す(タイムスタンプ・ラベルまで実データと一致)、メタデータ無しの呼び出し→`UNAUTHENTICATED`
- 外部公開API: 実際にKeycloakへ`client_credentials`グラントでトークンを取得し、offset方式(`page`/`page_size`)・cursor方式(フラグを一時ONにして`next_cursor`の連鎖まで確認)の両方が正しいレスポンスを返すことを確認済み。ローカルHMACトークン(`azp`一致・署名も正当)を渡した場合は`401`になることも確認済み

## 実装時に判明した既知の差異(実機検証結果)

- **`UPDATE`文の`rowcount`が「マッチ行数」ではなく「変化行数」を返す(実バグとして発見・修正済み)**: aiomysql/PyMySQLは既定で`CLIENT_FOUND_ROWS`フラグを立てないため、MySQLは`UPDATE`のaffected rowsを「実際に値が変わった行数」として返す。`tasks`テーブルのDATETIME列は秒精度しか持たないため、同じ秒内に作成・更新すると(かつ他のSET対象列の値も偶然全て同じだと)`updated_at`を含め1列も値が変わらず、WHERE句は行にマッチしているのに`rowcount=0`になりうる。`TaskRepository.update()`は`rowcount==0`を「対象行が見つからない」の判定に使っているため、これを放置すると正当な更新が誤って`not_found`として扱われる。実際に`test_update_dedups_duplicate_label_ids`結合テストで再現し、`aiomysql.create_pool(..., client_flag=CLIENT.FOUND_ROWS)`で修正した(`app/main.py`・`tests/integration/db_fixture.py`参照)
- **aiomysqlはDATETIME列をタイムゾーン変換無しのナイーブなdatetimeとして返す(Java/Kotlinとの対比、実機検証で確認済み)**: JDBCの`ResultSet#getTimestamp().toLocalDateTime()`はJVMのシステムデフォルトタイムゾーンを経由して変換してしまう(Java/Kotlinで実際に見つかった落とし穴、+9時間ズレる)のに対し、aiomysqlは元から変換ロジックが介在せず、UTCの壁時計値をそのままナイーブな`datetime.datetime`として返す。同様に`google.protobuf.Timestamp#FromDatetime()`もナイーブなdatetimeをUTCとして扱う(システムのローカルタイムゾーン変換は行われない、実機で計算値を突き合わせて確認済み)。この2点により、Pythonでは他言語で見つかったタイムゾーンずれのバグ自体が最初から起こり得ない
- **`str.__len__()`が最初からUnicodeコードポイント数を返す(Java/Kotlin/JSとの対比)**: Java/Kotlin/JavaScriptの`length`はUTF-16コード単位数を返すため、基本多言語面外の文字(絵文字等)を含む名前でサロゲートペアが2としてカウントされる既知の落とし穴がある。Python 3の`str`はPEP 393のフレキシブル文字列表現により常にコードポイント単位で格納されるため、この落とし穴自体が存在しない(`app/domain/validation.py`参照)
- **protoc生成コードが`task.v1`をトップレベルパッケージとしてimportする**: `grpc_tools.protoc`の既定の挙動で、生成された`task_pb2_grpc.py`は`from task.v1 import task_pb2`という、`app/generated`ディレクトリ自体をimportルートとして扱うコードを生成する。`app/generated_path.py`でこのディレクトリを`sys.path`へ追加している(gRPC関連モジュールをimportする前に必ず実行される)
- **pytest-asyncioの既定動作: テスト関数ごとに新しいイベントループを使う**: gRPCチャンネル/サーバー/DBコネクションプールのようにイベントループに紐づくオブジェクトをmodule/sessionスコープのfixtureで共有すると、「別のループに属するFutureを待っている」という`RuntimeError`になる(実際に再現した)。`tests/integration/test_grpc_service.py`の全fixtureを関数スコープ(既定)にし、テストごとに新しいサーバー/プール/チャンネルを作る設計で解決した

## 未実装・今後の予定

- bff/gateway/migrationへの配線(`backend.task-language`への`python`の追加)
