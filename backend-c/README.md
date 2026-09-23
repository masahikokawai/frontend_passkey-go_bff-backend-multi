# backend-c

bff-gin: Task CRUD backendのC実装(CONTRACT.mdセクション20の多言語backend比較の9言語目)

内部REST v1・内部gRPC v2のCRUD、JWT/JWKS認証(ローカルHMAC/ローカルRSA/Keycloakの3issuer)、外部公開API、Feature Flagポーリングを実装している。bff/gateway/migrationへの配線も完了しており(`backend.task-language`の`c`)、他8言語と同じ形でTask CRUD backendの1つとして切り替えられる。

## ポート

- 内部REST v1: `:8106`(環境変数`HTTP_ADDR`。backend-cppと同じくポート番号のみを受け付ける簡略形式)
- 内部gRPC v2: `:9100`(環境変数`GRPC_ADDR`、同じくポート番号のみ)
- 外部公開API: `:8110`(環境変数`EXTERNAL_HTTP_ADDR`、同じくポート番号のみ)

## 認証について

REST/gRPCともに、本物のJWT/JWKS検証(`X-Debug-User-Id`のようなデバッグ用ヘッダは無い)。backend-rust(`src/auth/mod.rs`)・backend-cpp(`src/auth/jwt.{hpp,cpp}`)と同じ3issuer構成を`src/auth/`にCで実装している:

- **ローカルHMAC**(`iss=bff-gin-local-hmac`): HS256、共有シークレット(`LOCAL_AUTH_HMAC_SECRET`)
- **ローカルRSA**(`iss=bff-gin-local-rsa`): RS256、JWKSはbff自身の`/.well-known/jwks.json`(`LOCAL_AUTH_RSA_JWKS_URL`)から取得
- **Keycloak**: RS256、JWKSはKeycloakの`/protocol/openid-connect/certs`(`KEYCLOAK_ISSUER`から導出、`KEYCLOAK_JWKS_URL`で上書き可)から取得

`Authorization: Bearer <token>`ヘッダ(gRPCは`authorization`メタデータ)を、署名検証前に`iss`だけ覗いて対応するVerifierへ振り分ける`Dispatcher`(`src/auth/dispatcher.c`)→実際の署名/`exp`/`aud`検証を行う`HmacVerifier`/`JwksVerifier`という2段構造(他言語と同じ設計)。認証ヘッダが無い、またはいずれの検証にも通らない場合はREST `401 {"error":"unauthenticated"}`・gRPC `UNAUTHENTICATED`。user_id解決(`src/auth/user_resolver.c`)はローカル発行issuerなら`sub`をそのまま`users.id`として、Keycloak発行issuerなら`sub`(keycloak_sub)を`user_keycloaks`テーブル経由で引く(backend-cppの`ResolveUserIdFromAuthHeader`と同じ分岐)。

**JWTライブラリを使わずOpenSSLのプリミティブを直接使う理由**: 成熟したC言語向けJWTライブラリが存在しないため(backend-cppも同じ理由で`jwt-cpp`を使わずOpenSSL直叩きを選んでいる、backend-cpp/README.md「JWTライブラリの選定」参照)。base64url decode(`src/auth/base64url.c`)・HMAC-SHA256検証(`HMAC()`)・RSA/JWKS検証(`BN_bin2bn`+`OSSL_PARAM_BLD`+`EVP_PKEY_fromdata`+`EVP_DigestVerify`、OpenSSL 3 API)を全て自前で実装しており、C++版が同じ処理をRAII/`std::expected`で書いていたのに対し、Cでは`create`/`destroy`関数ペア+`enum TaskError`で書く追加の比較教材になっている。

**セキュリティ上の注意点(自作JWT検証で特に重要)**:
- **アルゴリズムホワイトリスト**: JWTヘッダの`alg`は攻撃者が自由に書き換えられる値であり、これを鵜呑みにして検証方式を選ぶとアルゴリズム混同攻撃(`alg:none`等)を許してしまう。`HmacVerifier`/`JwksVerifier`はそれぞれ自分が担当するアルゴリズム(HS256/RS256)をコード側で固定し、ヘッダの値と完全一致しない限り無条件に拒否する(`src/auth/hmac_verifier.c`・`src/auth/jwks_verifier.c`のコメント参照)
- **定数時間比較**: HMAC署名の比較には`memcmp`/`strcmp`ではなく`CRYPTO_memcmp`(OpenSSLの定数時間比較関数)を使う。通常の比較関数は不一致位置で早期リターンするため、比較にかかる時間差から署名を推測されるタイミングサイドチャネル攻撃(CWE-208)を許しうる

JWKSはkid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけ再取得する「kid不一致時のみ再取得」戦略(`src/auth/jwks_verifier.c`のfind_key_copy→refresh→再find_key_copy、他言語と同じ)。JWKS取得はシステムlibcurl(macOSのXcode SDKに同梱、`find_package(CURL REQUIRED)`)で行う。

## 外部公開API・Feature Flagポーリング

- `src/flags/feature_flag_poller.{h,c}`: 専用のバックグラウンドpthreadが`feature_flags`テーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、backend-rust/backend-cppと同じ設計)。`pthread_mutex_t`で保護した固定長配列キャッシュに`flag_key`/`enabled`/`default_variation`を保持し、`feature_flag_poller_variation()`が「無効または未登録ならdefault_value、有効ならdefault_variation」を返す
- `src/external/external_handler.{h,c}` + `src/external/external_query.{h,c}`(クエリパース部分を分離): `GET /external/v1/tasks`。内部REST v1・gRPC v2とは別ポート(`:8110`)の独立したCivetWebリスナーで待ち受ける(認証モデルが異なるため、パスを追加するのではなく別リスナーにしている)
- 認証(`src/auth/external_auth.{h,c}`の`auth_require_external_client`): Client Credentials Grant(Keycloak発行)のみ受け付ける。`Dispatcher`で署名検証した後、`auth_is_local_issuer(claims->iss)`がtrueなら拒否(ローカルHMAC/RSAは内部REST/gRPC専用)、`claims->azp`が`EXTERNAL_API_CLIENT_ID`(既定`external-api-client`)と一致しなければ拒否する。`user_id`はクエリパラメータをそのまま使い、JWTの`sub`とは突き合わせない(この資格情報を持つ者は任意ユーザーのタスクを読み取れる、CONTRACT.mdセクション11に明記された既知の設計)
- `backend.external-tasks-pagination-v2`(全言語で共有する1つのFeature Flag)でoffset(v1、既定)/cursor(v2)を切り替える:
  - v1: `page`(既定1)/`page_size`(既定10)。`task_repository_list`をそのまま再利用する(offset = (page-1)\*page_size)。レスポンス`{"tasks":[...],"page":N,"page_size":N,"total":N}`
  - v2: `cursor`(省略時は先頭から)/`limit`(既定10)。gRPC v2のカーソルページングと同じ`task_repository_list_cursor`(id昇順のkeyset pagination)をそのまま再利用する。レスポンス`{"tasks":[...],"next_cursor":"文字列"|null,"limit":N}`。cursorはid単体の簡略設計(created_at+idの複合カーソルではない、backend-cppの外部公開APIと同じ簡略化)
- Task JSONは内部REST v1と同じ形状(`user_id`を含まない)を再利用している。内部REST v1側が元々`user_id`を含まない実装だったため、外部公開API向けに別の変換関数を新設する必要が無かった

## アーキテクチャ選定

### (a) HTTPサーバーにCivetWebを選んだ理由

backend-cppはBoost.Beast(非同期、`io_context`)を採用したが、Cにはコルーチンも`io_context`のような非同期ランタイムも標準では存在しない。CivetWebは固定サイズのワーカースレッドプール+ブロッキングI/Oというシンプルなモデルで、`libmysqlclient`の同期APIをそのまま各ワーカースレッドで呼び出せる。

これは意図的にbackend-cppと対称的な設計になっている:

```
C++: Boost.Beast(非同期) + 専用db_thread_poolへのディスパッチ(同期DBアクセスの隔離が必要)
C:   CivetWeb(同期・スレッドプール) + スレッドローカルDB接続(ディスパッチ自体が不要)
```

C++版はHTTP用の`io_context`スレッドをDBのブロッキング呼び出しで止めないために、`RunBlocking`ヘルパーで別スレッドプールへ明示的に処理を移す複雑さが必要だった。C版はCivetWebのワーカースレッド自体がブロッキング前提のモデルであるため、そのディスパッチの複雑さがそもそも発生しない。「Cは非同期ランタイムを持たない分、この種の設計問題そのものを回避できる」という対比自体に学習価値がある。

HomebrewにCivetWebのformulaが無いため、`FetchContent`(GitHub `civetweb/civetweb` v1.16)で取得している。TLS/C++ラッパー/テスト/サンプル実行ファイルは全て無効化し(`CIVETWEB_ENABLE_SSL/CXX/BUILD_TESTING/ENABLE_SERVER_EXECUTABLE`を`OFF`)、純粋なC HTTPサーバーライブラリとしてのみ利用している。

### (b) スレッドローカルMySQL接続(コネクションプールを自作しない設計)

backend-cppは`ConnectionPool`(RAIIリース)を自作したが、backend-cはコネクションプールを自作せず、CivetWebの各ワーカースレッドに`pthread_key_t`で1本ずつMySQL接続をキャッシュする設計にした(`src/db/mysql_conn.c`)。

```c
static void destroy_thread_connection(void *ptr) {
    if (ptr != NULL) mysql_close((MYSQL *)ptr);  /* スレッド終了時に自動的に呼ばれる */
}
static void make_key(void) {
    pthread_key_create(&g_conn_key, destroy_thread_connection);
}
```

`pthread_key_create`の第2引数(デストラクタ)は、そのキーに値をセットしたスレッドが終了する際に自動的に呼び出される。CivetWebはワーカースレッドの数が`num_threads`オプションで固定されているため、「ワーカースレッドの生存期間 = 接続の生存期間」という単純な1:1対応が成り立ち、明示的なプール(貸出/返却のロジック)を書く必要がない。

これはC++のRAII(オブジェクトのスコープに紐づく自動解放)と対比すると、「スレッドのライフタイムに紐づく自動解放」と言える。C++版はリクエスト単位で接続を借りて返す(`Lease`オブジェクトのデストラクタで返却)のに対し、C版はスレッド単位で接続を握りっぱなしにする(リクエストが終わっても閉じない)という違いがあり、真にプールを自作する場合と比べて「アイドル接続数の上限を細かく制御できない」というトレードオフがある(このプロジェクトの規模・学習目的では許容範囲と判断した)。

**Feature Flagポーリングスレッドでの対比**: backend-cpp(`src/flags/feature_flag_poller.cpp`)はポーリングのたびに`mysql_init`/`mysql_real_connect`/`mysql_close`を繰り返す。これはC++版のコネクションプール(リクエスト単位のRAIIリース)が「長寿命のバックグラウンドスレッド専用の1本だけの接続」という用途にはそのまま使えないための設計。backend-cの`mysql_conn_get()`は元々「呼び出したスレッドに接続を紐づける」という汎用の仕組み(CivetWebのワーカースレッド専用ではない)であるため、このポーリング専用スレッドからもそのまま呼べば良く、接続はスレッドが生きている間(プロセス終了まで)使い回される(`MYSQL_OPT_RECONNECT`により一時的な切断からも自動復帰する)。スレッドローカル接続という設計そのものが、C++のRAIIプールより長寿命スレッドに対して自然に適合する一例になっている。

### (c) gRPC: gRPC Core C APIを直接叩く方針

**gRPC C++ラッパー(`grpcpp/grpcpp.h`)は使わず、`grpc/grpc.h`のgRPC Core C APIを直接叩いている**(`src/grpc/grpc_server.c`)。C++版をラップして使うと「C実装」としての純度が損なわれるため、あえて低レベルなC APIを直接扱っている。

**メッセージのシリアライズ(protobuf-c)**: 公式`protoc`はC言語のコード生成に対応していないため、第三者ライブラリの[protobuf-c](https://github.com/protobuf-c/protobuf-c)で`.proto`から`*.pb-c.h`/`*.pb-c.c`を生成している(`CMakeLists.txt`のカスタムコマンド)。

> **protobuf-cの既知の制約**: `protoc-gen-c`(1.5.2時点)はproto3の`optional`フィールドに対応していない。全言語共通の正本`proto/task/v1/task.proto`は他言語と1文字も違わず(`optional string description`を含む)、これを変更するわけにはいかないため、ビルド時に`optional`だけを取り除いた一時コピーを生成し、それだけを`protoc-gen-c`へ渡している(正本のファイル自体は変更しない)。この回避策により、`description`について「明示的な空文字列」と「未設定」をワイヤー上で区別できなくなる(どちらも送信されない)という意味論上の副作用があるが、本プロジェクトでは実害のあるエッジケースではないため許容している。

**RPCルーティング**: protobuf-cはメッセージの(de)serializeのみを提供し、gRPCサービス定義からのRPCスタブ生成機能は無い(他7言語が使っている生成済みスタブに相当するものが存在しない)。代わりに`grpc_server_register_method`で5つのRPC(`/task.v1.TaskService/{ListTasks,GetTask,CreateTask,UpdateTask,DeleteTask}`)をメソッドパス文字列で明示的に登録し、`grpc_server_request_registered_call`で着信を待つ「registered method」方式にしている。この文字列自体はコード生成の産物ではなく、`.proto`の`package`/`service`/`rpc`宣言から自分で書き起こしたものであり、「スタブが無いので手でルーティングを書く」という学習目的はこの方式でも変わらない。

**サーバー起動〜リクエスト処理の流れ**(`grpc_server_module_start`): `grpc_server_create` → 5メソッド分`grpc_server_register_method` → `grpc_completion_queue_create_for_next` → `grpc_server_register_completion_queue` → `grpc_server_add_http2_port`(`grpc_insecure_server_credentials_create`、TLS未実装) → `grpc_server_start` → メソッドごとに`grpc_server_request_registered_call`で着信登録 → 4本のpthreadワーカーが`grpc_completion_queue_next`のループを回す。着信を受け取ったら即座に次の着信も登録し直す(再アーム)ことで、1件の処理中でも後続の着信を受け付けられるようにしている。

**`grpc_op`バッチ**: `GRPC_SRM_PAYLOAD_READ_INITIAL_BYTE_BUFFER`で登録しているため、着信イベントの時点でリクエストのバイト列は既に読み込み済みになる(別途`RECV_MESSAGE`バッチを組む必要が無い)。応答は`SEND_INITIAL_METADATA`+`SEND_MESSAGE`+`SEND_STATUS_FROM_SERVER`を1つのバッチにまとめて`grpc_call_start_batch`に渡す。`RECV_CLOSE_ON_SERVER`(cancel検知用)だけは別バッチにする必要がある(同じバッチに入れると、応答送信より先に完了しえないRECV_CLOSE_ON_SERVERのせいで、バッチ全体の完了通知そのものが遅れてしまうため)。gRPC C++の`ServerContext`/`ServerUnaryReactor`が隠蔽している部分を全て手動で扱っている。

**3系統のメモリ管理**: `grpc_slice`/`grpc_byte_buffer`(参照カウント、`grpc_slice_unref`/`grpc_byte_buffer_destroy`)、protobuf-cのメッセージ(`*__free_unpacked`。手動構築したメッセージにもそのまま使える、`src/grpc/task_mapper.c`参照)、このプロジェクト自身のドメインオブジェクト(`task_create`/`task_destroy`)という独立した3つのメモリ管理系統を、リクエスト処理の1パスの中で正しく対応させる必要がある(下記「メモリ管理」参照)。

**エラー変換**: `enum TaskError` → `grpc_status_code`の変換関数(`task_error_to_grpc_code`、backend-cppの`AppErrorKind`→`grpc::StatusCode`と同じ対応表)。

この構成は、他の7言語のgRPC実装(いずれも生成済みスタブ+高レベルなCallback/async APIを使っている)が隠している「HTTP/2フレーミング」「Completion Queueベースの非同期処理」「手動のRPCルーティング」「3階層の手動メモリ管理」を意図的に露出させる選択であり、C++版で得られた「RAIIがどこまで自動化してくれるか」という学習に対する裏返しの教材(「何も自動化されないとどれだけ手間がかかるか」)として設計している。

### (d) backend-cppとの比較表

| | backend-c(このフェーズ) | backend-cpp |
|---|---|---|
| HTTP | CivetWeb(同期・固定スレッドプール) | Boost.Beast(非同期、`io_context`) |
| MySQL接続 | `libmysqlclient`を生で使用(スレッドローカルにキャッシュ) | 同じ`libmysqlclient`をRAII(`std::unique_ptr`+カスタムデリータ)で包む |
| コネクション管理 | プール自作なし(`pthread_key_t`デストラクタでスレッド終了時に自動close) | 自作`ConnectionPool`(RAIIリース、デストラクタで返却) |
| メモリ管理 | 手動`create`/`destroy`関数ペア(例: `task_create`/`task_destroy`) | スマートポインタ・`std::string`・`std::vector`によるRAII |
| エラー処理 | `enum TaskError`の戻り値 | `std::expected<T, AppError>` |
| JSON | cJSON(`cJSON_CreateObject`等、手動でメモリ解放) | `nlohmann::json`(値型、自動管理) |
| 文字列 | `char *`(所有権はコメント+規約で管理) | `std::string`(所有権は型システムとRAIIで保証) |
| バリデーション | UTF-8コードポイント数の自作カウンタ、`gmtime_r`によるUTC日付比較 | 同じロジックをC++の`std::string`操作で実装(値は同一) |
| ビルド | CMake + Homebrew(mysql-client/cJSON/gRPC/Protobuf/protobuf-c) + FetchContent(CivetWeb/Unity) | CMake + Homebrew(Boost/mysql-client/nlohmann_json/gRPC/Protobuf) |
| テスト | Unity | GoogleTest |
| gRPC実装 | gRPC Core C API直叩き(`grpc/grpc.h`)、`grpc_server_register_method`+completion queue+pthreadワーカーを自前実装 | gRPC C++ Callback API(`grpc::CallbackServerContext`) |
| gRPCメッセージ生成 | protobuf-c(`protoc-gen-c`、メッセージのみ、RPCスタブ無し) | 公式`protoc --cpp_out --grpc_out`(メッセージ+RPCスタブ両方) |

## メモリ管理

Cには例外もRAIIも無いため、メモリ/リソース管理は全て手動の規約(create/destroyペア、所有権コメント)で保証する。以下は「素朴に書くと壊れる例」をコメントアウトで残し、その直後に採用した安全な実装を並べている箇所の一覧(実際にこのコードで過去に発生したバグではなく、C特有の落とし穴を教材として明示するための対比):

| ファイル・関数 | 素朴な実装だと何が起きるか | 採用した実装 |
|---|---|---|
| `src/repository/task_repository.c`の`task_repository_delete` | `tasks`と`task_labels`を別々にDELETEすると、途中でクラッシュ/切断した場合に孤立行が残る(backend-rustで実際に見つかった既知バグと同種) | `mysql_autocommit(conn, 0)`→両方のDELETE→`mysql_commit(conn)`(失敗時`mysql_rollback`) |
| `src/repository/task_repository.c`の`attach_labels` | 固定長スタックバッファ+`strcat`だと、IN句の対象件数が多い場合にバッファオーバーフローする(CWE-121) | 件数から必要バイト数を計算してから`malloc`する(上限なし) |
| `src/domain/task.c`の`task_set_name` | 固定長バッファ+`strcpy`だと長いnameでオーバーフローする(CWE-120)。さらに「先にfree、後でstrdup」の順序だと`strdup`失敗時にdangling pointerが残る | `strdup`でヒープに複製(上限なし)。既存値のfreeは新しい複製の確保に成功した後に行う |
| `src/http/handler.c`の`read_body` | 残り容量を渡さずに`mg_read`を呼ぶと、想定より大きいボディでヒープバッファがオーバーフローする(CWE-787) | 毎回「残り容量」を計算して渡し、容量が尽きたら明示的にループを打ち切る |
| `src/db/mysql_conn.c`の`make_key`(`pthread_key_create`) | デストラクタ引数にNULLを渡すと、スレッド終了時にMySQL接続(fd)が自動解放されずリークし続ける(CWE-772) | `destroy_thread_connection`をデストラクタとして渡し、スレッド終了時に`mysql_close`を自動的に呼ばせる |
| `src/grpc/grpc_server.c`の`resolve_user_id_from_metadata` | `grpc_slice_to_c_string`は`gpr_malloc()`で確保したバッファを返す(grpc-core自身のアロケータ)。これを通常の`free()`で解放すると、gprとlibcのアロケータ実装が食い違うビルド構成では未定義動作になる(異なるアロケータ間でポインタを混用する典型的バグ) | `gpr_free()`で解放する(`gpr_malloc`/`gpr_free`は常にペアで使う) |
| `src/auth/hmac_verifier.c`のHMAC署名比較 | `memcmp`/`strcmp`は不一致位置で早期リターンするため、比較にかかる時間差から署名を推測されるタイミングサイドチャネル攻撃(CWE-208)を許しうる | OpenSSLの`CRYPTO_memcmp`(定数時間比較関数)を使う |
| `src/auth/jwt.c`のalgホワイトリストチェック | JWTヘッダの`alg`を鵜呑みにして検証方式を分岐すると、`alg:none`等によるアルゴリズム混同攻撃を許してしまう | 各Verifierが担当するアルゴリズム(HS256/RS256)をコード側で固定し、ヘッダの値と完全一致しない限り無条件に拒否する |
| `src/auth/base64url.c`の`base64url_decode` | ビット累積用の`val`を符号付き`int`のまま一度も縮小せず左シフトし続けると、入力が数文字を超えたあたりで符号付き整数オーバーフロー(未定義動作、実際にUndefinedBehaviorSanitizerで検出) | `val`を符号無し(`uint32_t`)にし、バイトを1つ取り出すたびに使用済みビットをマスクして縮小する |
| `src/auth/jwks_verifier.c`のJWKSレスポンスバッファ(`curl_write_cb`) | `realloc`の戻り値を元のポインタへ直接代入すると、失敗時(NULL)に元のバッファを指すポインタ自体を失い解放できなくなる(CWE-401) | 一時変数で受け取り、成功を確認してから初めて元のポインタへ反映する |
| `src/auth/jwks_verifier.c`の`build_rsa_public_key`/`jwks_verifier_verify` | `EVP_PKEY_free`を成功時パスにしか置かないと、署名不一致(攻撃者が容易にトリガーできる)のたびに`EVP_PKEY`がリークする(CWE-401) | 成功/失敗どちらのパスでも使い終わったら必ず`EVP_PKEY_free`を呼んでから分岐する |
| `src/grpc/grpc_server.c`の3系統のメモリ管理 | `grpc_slice`/`grpc_byte_buffer`(参照カウント)・protobuf-cのメッセージ(`*__free_unpacked`)・ドメインオブジェクト(`task_create`/`task_destroy`)を混同すると、二重解放またはリークになる | 各系統の解放関数を対応する生成元でのみ呼ぶ(`pack_task`はprotobuf-cメッセージをpackしたら即座に`free_unpacked`、`grpc_byte_buffer`は送信完了(`tag_finish`)まで保持してから`grpc_byte_buffer_destroy`) |

AddressSanitizer/UndefinedBehaviorSanitizerでのビルド・検証:

```sh
cmake -S . -B build-asan -DENABLE_ASAN=ON
cmake --build build-asan -j 4
./build-asan/backend_c_tests
./build-asan/backend_c_jwt_tests
./build-asan/backend_c_jwks_tests
./build-asan/backend_c_external_query_tests
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build-asan/backend_c_integration_tests
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build-asan/backend_c_grpc_integration_tests
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build-asan/backend_c_feature_flag_poller_tests
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build-asan/backend_c_external_integration_tests
```

`backend_c_lib`(および同ライブラリをリンクする全ターゲット)にのみ`-fsanitize=address,undefined`を付与する(`CMakeLists.txt`の`ENABLE_ASAN`オプション参照)。CivetWeb/Unity(FetchContentで取得する依存先)はサニタイザ無しのままビルドされるが、サニタイザ有無が混在したコード同士のリンク自体は標準的にサポートされている。実機で単体テスト・両方の結合テスト(REST/gRPC)を実行し、AddressSanitizer/UndefinedBehaviorSanitizerの警告が0件であることを確認済み(macOSのASanは`detect_leaks`非対応のためリーク検出はUBSan/ASanのメモリ破壊検出のみでの確認)。

## 結合テスト

### REST(Repository層直接)

`tests/task_repository_integration_test.c`は、docker-compose上の実MySQLに接続してRepository層(`task_repository_*`関数群)を直接検証する結合テスト(HTTP層を経由しない、backend-rustの`tests/integration_test.rs`と同じ考え方)。`backend_c_tests`(Unity、DB接続不要な単体テストのみ)とは別の実行ファイルにしており、`ctest`のデフォルト対象には含めない(`add_test()`未登録、`ctest`実行のたびにdocker composeを要求しないため)。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
cmake --build build -j 4
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build/backend_c_integration_tests
```

テストごとに一意なemail/keycloak_sub/label名でユーザー・ラベルを作り、Unityの`tearDown`で確実に削除する(`TEST_ASSERT`失敗によるlongjmp後も`tearDown`は呼ばれるため、アサート失敗時でも共有の開発用DBに後始末漏れの行が残らない)。カバー範囲: create/find/update/delete一連の往復、delete時の`task_labels`孤立行防止(トランザクション保護)、重複label_idの正規化、他ユーザーのtaskへの不可視性(所有権分離)、JWT認証のuser_id解決に使う`task_repository_find_user_by_id`/`task_repository_find_user_by_keycloak_sub`(usersテーブルとuser_keycloaksテーブルのJOIN経由)。実機で全6件のpass、および結合テスト前後で開発用DBに`integration-c-`プレフィックスの行が残らないことを確認済み。

### gRPC(ワイヤーまで含む)

`tests/task_grpc_integration_test.c`は、このテストプロセス自身の中で実際に`grpc_server_module_start`(`src/grpc/grpc_server.c`)を起動し、`tests/grpc_test_client.c`(サーバーと同じgRPC Core C APIを直接使って書いた自作の同期クライアント)からgRPCのワイヤーまで含めてRPCを送り、実DBまで到達させる結合テスト。デフォルトで`:19100`を使う(`GRPC_TEST_PORT`環境変数で変更可、デフォルト起動する`backend_c_server`の`:9100`とは別ポート)。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
cmake --build build -j 4
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build/backend_c_grpc_integration_tests
```

実際に検証可能な署名済みJWT(`tests/test_token_helper.c`のmake_hmac_token、このテスト専用のローカルHMACシークレットで`auth_module_init`する)を`authorization: Bearer <token>`メタデータとして送る。カバー範囲: create/get/update/delete一連の往復、delete時の`task_labels`孤立行防止(トランザクション保護、gRPC経由でも同じRepositoryを共有していることの確認)、`authorization`メタデータ無しの呼び出しが`UNAUTHENTICATED`になること、期限切れJWTでの呼び出しが`UNAUTHENTICATED`になること、idのkeyset cursorによるページネーション(2ページ目・「次ページ無し」判定)。実機で全5件のpass、AddressSanitizer/UndefinedBehaviorSanitizerビルドでの警告0件、および実際に起動した`backend_c_server`に対する`grpcurl`での5RPC全ての手動確認(下記「動作確認」参照)を実施済み。

### Feature Flagポーリング

`tests/feature_flag_poller_test.c`は、実DBに一意なテスト専用`flag_key`の行を挿入し、`feature_flag_poller_poll_once_for_test()`(10秒間隔を待たない即時ポーリング、テスト専用のエントリポイント)経由で`feature_flag_poller_variation()`のフォールバック規則(enabled=true→default_variation、enabled=false→呼び出し側のdefault_value、flag_key未登録→同じくdefault_value)を検証する。他の機能が参照する既存のflag行には一切触れず、テストごとに一意な`flag_key`を使う(共有の開発用DBを汚さない)。実機で全3件のpassを確認済み。

### 外部公開API

`tests/external_handler_integration_test.c`は、実DB・モックJWKSサーバー(`tests/jwks_test.c`と同じ手法で自プロセス内に構築、Keycloak相当として扱う)・外部公開API自身のCivetWebリスナーの3つを1プロセス内に立て、libcurlで実際にHTTP GETを送ってエンドツーエンドに検証する。カバー範囲: `Authorization`ヘッダ無し→401、`azp`不一致のトークン→401、`user_id`欠如→400、v1(offset)ページングがページ境界をまたいで正しく動くこと(`page`/`page_size`/`total`)、v2(cursor)ページングが`next_cursor`を正しく連鎖させ最終ページで`null`になること。`backend.external-tasks-pagination-v2`は退避→書き換え→テスト後に復元する(全言語で共有する1つのFeature Flagのため、他のテスト・実機動作に影響を残さない)。実機で全5件のpassを確認済み。

`tests/jwks_test.c`にも、実サーバーを起動せず`auth_require_external_client`を直接呼ぶ3件の単体テストを追加している(Keycloak相当のトークン+一致する`azp`→受理、`azp`不一致/欠如→拒否、正しく署名されたローカルHMACトークン→issがローカルという理由だけで拒否)。

## セットアップ

```sh
# 依存関係(Homebrewに無いCivetWeb/Unityはビルド時にFetchContentで自動取得される。
# JWKS取得用のlibcurlはmacOSのXcode SDKに同梱のものをそのまま使うためbrew install不要)
brew install cmake cjson mysql-client grpc protobuf protobuf-c openssl@3 grpcurl

# ビルド
cd backend-c
cmake -S . -B build
cmake --build build -j 4

# テスト
./build/backend_c_tests

# 起動(docker composeでmysql/keycloakが起動済みであること)
HTTP_ADDR=8106 GRPC_ADDR=9100 EXTERNAL_HTTP_ADDR=8110 DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build/backend_c_server

# 別ターミナルからgRPCの動作確認(grpcurl、ローカルHMAC発行の署名済みJWTを"Bearer <token>"で渡す。
# トークンの組み立て方はtests/test_token_helper.cのmake_hmac_token参照)
grpcurl -plaintext -proto proto/task/v1/task.proto -import-path proto -import-path /opt/homebrew/include \
  -H "authorization: Bearer <token>" -d '{"limit":5}' localhost:9100 task.v1.TaskService/ListTasks

# 外部公開APIの動作確認(Keycloakからclient_credentialsグラントでトークンを取得して渡す)
KC_TOKEN=$(curl -s -X POST "http://localhost:8082/realms/training/protocol/openid-connect/token" \
  -d "grant_type=client_credentials" -d "client_id=external-api-client" \
  -d "client_secret=external-api-client-local-dev-secret" | jq -r .access_token)
curl -H "Authorization: Bearer $KC_TOKEN" "http://localhost:8110/external/v1/tasks?user_id=1&page=1&page_size=10"
```

### ORM禁止・migrationsディレクトリを持たない

`src/repository/task_repository.c`で生SQL(`mysql_stmt_*`によるPrepared Statement)を直接書いている。スキーマ正本は`backend/migrations`のみであり、このC実装は独自の`migrations/`ディレクトリを持たず、既存の`tasks`/`task_labels`/`labels`テーブルをそのまま読み書きする。

### 削除のトランザクション保護

`task_labels`に外部キー制約は無いため、`tasks`と`task_labels`を別々に(トランザクション無しで)DELETEすると、片方だけ成功して孤立行が残り得る(backend-rustで見つかった既知バグと同種の問題)。backend-c版は`task_repository_delete`内で`mysql_autocommit(conn, 0)` → `DELETE FROM tasks` → `DELETE FROM task_labels` → `mysql_commit(conn)`とし、いずれかが失敗したら`mysql_rollback(conn)`する設計にしている(backend-cpp・backend-rust(修正後)・他6言語と同じ設計)。削除は物理削除(ソフトデリートフラグは無い)。

## 各種ログの出力先

標準出力(ターミナル)にkey=value形式で出力する。REST v1・外部公開API・gRPCの3トランスポートいずれも、実際に送信したHTTPステータス/gRPCステータスをそのまま記録する(推測・再計算しない)

- `rest method=<METHOD> path=<PATH> status=<実際のHTTPステータス> duration_ms=<経過ms>`(`src/http/handler.c`)
- `external method=<METHOD> path=<PATH> status=<実際のHTTPステータス> duration_ms=<経過ms>`(`src/external/external_handler.c`)
- `grpc method=<gRPCメソッドパス> status=<gRPCステータスコード> duration_ms=<経過ms>`(`src/grpc/grpc_server.c`、既存)

環境変数`LOG_LEVEL`(既定`info`)に`debug`を指定すると、上記の1行サマリに加えて以下のデバッグ行が追加で出力される(第三者ロギングライブラリは使わず、`src/common/log.c`の`log_debugf`が有効/無効を切り替えるだけの最小限の実装):

- 認証成功時に解決した`user_id`
- REST v1のクエリパラメータ(`limit`/`offset`)、外部公開APIのページング指定(`page`/`page_size`または`after_id`/`limit`)
- JWKSキャッシュの再取得(kid不一致によるrefresh)が発生したタイミング

標準出力は既定で完全バッファリングされる(ターミナル以外へリダイレクトした場合)ため、`main()`冒頭で`setvbuf(stdout, NULL, _IOLBF, 0)`により行バッファリングへ切り替え、各ログ行が発生した瞬間にファイル/パイプへ書き出されるようにしている

## 動作確認(実機で確認済み)

- `cmake -S . -B build && cmake --build build -j 4`: 通ることを確認済み(AppleClang 21、`-DCMAKE_POLICY_VERSION_MINIMUM`をCivetWeb取得時のみ一時的に設定してCMake互換性エラーを回避、`CIVETWEB_ENABLE_ASAN`をOFFにしてUBSanシンボル未解決のリンクエラーを回避)
- `ctest`(Unity、DB・Keycloak不要な4実行ファイル): 全てpass。`backend_c_tests`7件(ステータス変換・カレンダー上不正な日付の判定・UTF-8コードポイント長・UTC今日判定・バリデーション3件)、`backend_c_jwt_tests`8件(ローカルHMAC検証・alg混同攻撃拒否・Dispatcherのissuerルーティング/未知issuer拒否等)、`backend_c_jwks_tests`6件(自プロセス内にCivetWebでモックJWKSサーバーを立て、RS256の実署名検証・未知kid拒否・issuer不一致拒否・外部公開API用の`azp`検証3件を確認)、`backend_c_external_query_tests`12件(外部公開APIのクエリパース処理の純粋関数テスト)
- 実機(docker-compose上のMySQL、`docker compose ps`でmysql/redis/keycloak/swagger-uiが起動済みであることを確認した上で)に対して、実際に`HTTP_ADDR=8106`でサーバーを起動し、以下を`curl`で確認済み:
  - 認証ヘッダ無し → `401 {"error":"unauthenticated"}`
  - 実際に署名した有効なローカルHMAC JWT(`Authorization: Bearer <token>`) → `GET /internal/v1/tasks`が既存データを正しいJSON形状(`id`/`name`/`description`/`status`/`finished_on`/`labels`/`created_at`/`updated_at`、日時は`YYYY-MM-DDTHH:MM:SS+00:00`形式)で返す
  - 期限切れのローカルHMAC JWT、および正しいJWT形式でないbearerトークン → いずれも`401 {"error":"unauthenticated"}`
  - `POST /internal/v1/tasks`: `label_ids: [7,7,8]`(重複)で作成 → 201、レスポンスと`task_labels`テーブルの両方で`[7,8]`の2件に正規化されていることをSQLで直接確認
  - `GET /internal/v1/tasks/{id}`: 取得→ 200、存在しないid → 404、数値でないid(`/abc`) → 400 `invalid_id`
  - `PATCH /internal/v1/tasks/{id}`: `label_ids: [8,8,9]`(重複)で更新 → 200、`task_labels`が`[8,9]`の2件に正規化されることをSQLで確認、存在しないidの更新 → 404
  - `DELETE /internal/v1/tasks/{id}`: 削除 → 204(ボディなし)、再削除 → 404(冪等性確認)、削除後に`SELECT COUNT(*) FROM task_labels WHERE task_id=...`が0件であることをSQLで直接確認(トランザクション保護が機能している)
  - バリデーション: 名前20コードポイントちょうど → 成功(境界値)、finished_onが過去日(UTC基準) → 422 `validation_error`、finished_onがカレンダー上不正(`2026-02-30`) → 422 `invalid_finished_on`、必須フィールド欠如 → 400 `invalid_request`、不明なstatus文字列 → 422 `validation_error`(メッセージ付き)
  - 所有権分離: 他ユーザー(`sub`の異なるJWT)が所有するtaskを取得 → 404(存在自体を漏らさない)
- 確認後、起動したサーバープロセスは停止し、検証で作成したテストデータ(taskおよびtask_labels)は全て削除済み(削除確認自体を伴わない`bad-status`検証行のみ手動DELETEで削除、それ以外はAPI経由のDELETEで削除しSQLで0件を確認済み)
- gRPC: 実際に`GRPC_ADDR=9100`でサーバーを起動し、`grpcurl`(protoc生成コードを使わず`.proto`を直接読ませる、backend-cが生成するスタブを一切経由しない完全に独立したクライアント)で以下を確認済み:
  - `authorization: Bearer <token>`(実際に署名した有効なローカルHMAC JWT)付きの`CreateTask`: 201相当のTaskメッセージを返す(`id`採番、`createdAt`/`updatedAt`がRFC3339形式)
  - `GetTask`: 直前に作成したtaskを取得
  - `ListTasks`: 既存データ(他フェーズで作成した実データ)を正しいJSON形状で返す、`nextCursor`が付与される
  - `UpdateTask`: `status`変更が反映される
  - `DeleteTask`: 空メッセージ`{}`を返す
  - 削除後の`GetTask` → `NotFound`(`task not found`)
  - `authorization`メタデータ無しの呼び出し → `Unauthenticated`(`unauthorized`)
  - 確認後、検証で作成したtaskはAPI経由の`DeleteTask`で削除済み
- 外部公開API: 実際に`EXTERNAL_HTTP_ADDR=8110`でサーバーを起動し、Keycloakから実際に`client_credentials`グラントでトークンを取得して以下を`curl`で確認済み:
  - 実際に取得したKeycloakトークン(`azp=external-api-client`)でのv1(offset)リクエスト → 200、既存データを正しいJSON形状(`tasks`/`page`/`page_size`/`total`)で返す
  - 認証ヘッダ無し → 401 `{"error":"unauthenticated"}`
  - 正しく署名されたローカルHMAC JWT(内部REST/gRPCでは有効なトークン) → 401(issがローカルという理由で拒否されることを実機で確認)
  - `user_id`クエリパラメータ欠如 → 400 `{"error":"user_id_required"}`
  - `backend.external-tasks-pagination-v2`を一時的に有効化し、v2(cursor)リクエストで`next_cursor`が正しく次ページの起点(id)を指すことを確認(確認後、フラグは元の値に復元済み)
- リクエスト単位のログ: 上記REST v1・外部公開APIの各パターン(200/401/404等)で、実際に`rest method=... status=...`・`external method=... status=...`行が正しいステータスと共に出力されることを確認済み。`LOG_LEVEL=debug`で起動すると`rest debug: ...`/`external debug: ...`/`auth debug: jwks refresh ...`行が追加で出ること、`LOG_LEVEL`未設定(既定)ではこれらが一切出ないことの両方を実機で確認済み

## 実装時に判明した既知の差異(実機検証結果)

- **`my_bool`型がこのmysql-clientバージョンには存在しない**: backend-cpp実装時に判明していた既知の差異と同じで、`MYSQL_BIND.is_null`や`MYSQL_OPT_RECONNECT`オプションには`<stdbool.h>`の`bool`をそのまま使う必要がある
- **CivetWebのURLハンドラマッチングの挙動**: `mg_set_request_handler`は「完全一致」→「登録パス配下の部分一致(ワイルドカード不要)」→「パターンマッチ(`*`等)」の3段階で評価される。そのため`/internal/v1/tasks`(コレクション)と`/internal/v1/tasks/*`(個別リソース)を素朴に別々のハンドラとして登録すると、個別リソース向けのリクエストは常に2段階目でコレクションハンドラの部分一致に吸収されてしまい、ワイルドカードハンドラ(3段階目)には永遠に到達しない。`/internal/v1/tasks`1つだけにハンドラを登録し、`task_request_handler`内部でURIの残り部分を見てコレクション/個別リソースを振り分ける設計に変更して解決した(実機の`curl`検証でこの不具合を発見し、修正後に再検証して解決を確認済み)
- **CivetWeb本体のCMakeLists.txtが古い**: `cmake_minimum_required(VERSION 2.8...)`相当のため、手元のCMakeでは`cmake_minimum_required(VERSION < 3.5)`が拒否される。`FetchContent_Declare`でCivetWebを取得する直前だけ`CMAKE_POLICY_VERSION_MINIMUM`を`3.5`に設定して回避した
- **`mg_sleep`は公開APIに存在しない**: メインループのスリープは標準の`sleep(1)`(`<unistd.h>`)で代替した
- **CivetWebの`CIVETWEB_ENABLE_ASAN`が既定でON**: Debugビルドで`-fsanitize=undefined`相当のフラグが付与され、サニタイザ無しでビルドした自プロジェクトの実行ファイルとリンクする際にUBSanのシンボルが未解決になりリンクエラーになった。`CIVETWEB_ENABLE_ASAN OFF`をCache変数として強制することで解決した
- **gRPCのCMake config(`gRPCConfig.cmake`)はCXXコンパイラの機能検出を要求する**: `project(backend_c C)`のままだと`find_package(gRPC CONFIG REQUIRED)`が`No known features for CXX compiler`で失敗する。「gRPCのC API」は`grpc/grpc.h`というCから呼べるABIを指すだけで、gRPC Core自体の実装はC++で書かれているため、`project(backend_c C CXX)`とCXXも有効にする必要があった
- **protoc-gen-c(protobuf-c)はproto3の`optional`フィールドに未対応**: 全言語共通の正本`task.proto`が使う`optional string description`でコード生成自体が失敗する。上記「アーキテクチャ選定」(c)の対応で解決(正本は変更せず、ビルド時に`optional`を除いた一時コピーだけをprotoc-gen-cに渡す)
- **`grpc_server_cancel_all_calls`と`grpc_server_destroy`を別スレッドから競合して呼ぶとSEGVする**: `grpc_server_shutdown_and_notify`の通知タグは、実行中の呼び出しが無ければほぼ即座にcompletion queueへ積まれる。これを呼んだスレッドとは別の(completion queueを共有する)workerスレッドが先にそのタグを拾って`grpc_server_destroy`してしまい、その直後に元のスレッドが「既に破棄されたserver」に対して`grpc_server_cancel_all_calls`を呼んでクラッシュした(結合テストの`grpc_server_module_stop`実行時に実機で発生・再現・修正済み)。`grpc_server_cancel_all_calls`は「shutdown後にのみ呼べる」とドキュメントされている一方、`destroy`との前後関係は呼び出し側で保証する必要がある。修正: `cancel_all_calls`→`destroy`→`completion_queue_shutdown`を、通知タグを受け取った1つのworkerスレッドの中だけで順番に呼ぶように変更し、他スレッドとの競合を無くした(`src/grpc/grpc_server.c`の`worker_loop`/`grpc_server_module_stop`参照)
- **base64url decodeのビット累積で符号付き整数オーバーフロー**: `src/auth/base64url.c`の初期実装は、デコード中のビット累積用変数を符号付き`int`のまま一度も縮小せず左シフトし続けていたため、入力が数文字を超えたあたりで未定義動作(UndefinedBehaviorSanitizerが`left shift ... cannot be represented in type 'int'`として実際に検出)になっていた。変数を符号無し(`uint32_t`)にし、バイトを1つ取り出すたびに使用済みビットをマスクして縮小する実装に修正し、ASan/UBSanビルドで再検証して警告0件を確認した
- **成熟したC言語向けJWTライブラリが存在しない**: base64url decode・HMAC-SHA256検証・RSA/JWKS検証(OpenSSL 3の`EVP_PKEY_fromdata`系API)を全て自前実装する必要があった(上記「認証について」参照、backend-cppも同じ理由でOpenSSL直叩きを選んでいる)
- **JWKS取得はシステムlibcurlで問題なく動く**: Homebrewでの追加インストールは不要で、macOSのXcode SDKに同梱されているlibcurl(`curl-config`で確認可能)を`find_package(CURL REQUIRED)`でそのまま検出・リンクできた
- **`task_repository_list`(offsetページング)の`ORDER BY`にtie-breakが無かった**: `created_at DESC`のみだと、MySQLの`DATETIME`は秒精度のため短時間に複数作成したtaskが同値になり得て、offsetページングで同じ行が2ページにまたがって出現したり1件も出現しないまま飛ばされたりし得る(外部公開API v1のページ境界をまたぐ結合テストで顕在化)。`ORDER BY created_at DESC, id DESC`とし、一意なidをtie-breakに追加して解決した(backend-rustの`list_tasks_offset_external`と同じ`ORDER BY`規約)
