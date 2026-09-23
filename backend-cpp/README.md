# backend-cpp

bff-gin: Task CRUD backendのC++実装(CONTRACT.mdセクション20の多言語backend比較の1つ)

内部REST v1・gRPC v2・外部公開API・JWT認証(Keycloak/ローカルHMAC/ローカルRSAの3issuer)・Feature Flagポーリングを実装済み
bff・gateway・migrationへの配線も完了しており、`backend.task-language`を`cpp`に切り替えて実際に使える

## ポート

- 内部REST v1: `:8105`(環境変数`HTTP_ADDR`、既定値`8105`。他言語の`HTTP_ADDR`はホスト:ポート形式だが、このC++実装は簡略化のためポート番号のみを受け付ける)
- gRPC v2: `:9099`(環境変数`GRPC_ADDR`)
- 外部公開API: `:8109`(環境変数`EXTERNAL_HTTP_ADDR`)

## アーキテクチャ選定

### 検討した3案

| | ① Boost.Asio/Beast(採用) | ② Drogon | ③ Crow |
|---|---|---|---|
| HTTP | Beast(低レベル、フレームワークではない) | 内蔵(ルーティング・フィルタ・HTTPS) | 内蔵(Flask風ルーティング) |
| JSON | `nlohmann::json` | 内蔵(jsoncpp) | 内蔵 |
| DB | libmysqlclient(下記参照) | 内蔵ORM/非同期DB連携 | 外部ライブラリ任せ |
| gRPC | 公式`grpc++`(Callback API) | 別途`grpc++`並走 | 別途`grpc++`並走 |
| 並行処理モデル | `io_context`+C++20コルーチン | イベントループ+コルーチン | スレッドプール |
| 学習の焦点 | C++固有の所有権管理(RAII)・非同期統合パターン | 実務的なC++ Webフレームワークの使用感 | シンプルなルーティング |
| 実装コスト | 高(全て自分で組み立てる) | 低 | 中 |

### ①を採用した理由

将来Cで同じTask CRUDを実装した際の比較教材としての対称性を優先した。

```
C:   CivetWeb        / gRPC Core C API / MySQL C API      / pthread
C++: Boost.Beast     / gRPC C++        / MySQL C API(RAII包み) / std::thread/io_context
```

同じ抽象度(「フレームワークではなく部品を組み合わせる」)で両言語を並べられる。DrogonやCrowを選ぶと、ルーティング・JSON・DB連携がフレームワーク内部に隠れてしまい、「C++がCと比べて何を安全に自動化してくれるのか」という比較の解像度が下がる。

### ①を選んだことで薄れる学習効果(正直な記録)

②Drogonを選んでいれば、ルーティング・JSON・ORM・非同期DB連携が最初から統合されており、より少ないコード量で「実務でよく使われるC++ Webフレームワークの使用感」を学べたはずである。①では、セッション管理(`enable_shared_from_this`パターン)・ルーティング・DB接続プール・同期DBアクセスの隔離を全て自分で組み立てる必要があり、その分「フレームワークの中身がどう動いているか」への理解は深まるが、実装量・学習の立ち上がりコストは重くなる。今回のTask CRUDという小さな題材に対しては、率直に言ってオーバースペックな実装量になっている面がある。

## 【重要な設計変更】ライブラリ選定の実機検証結果

事前に合意していた設計から、実装時に判明した制約により2点変更している。

### 1. MySQL Connector/C++の「classic API」は現在のバージョンに存在しない

当初の想定は「MySQL Connector/C++のclassic(JDBC4ベース)API(`sql::Connection`等)」だったが、Homebrew配布の`mysql-connector-c++`(26.7.0)にはX DevAPI(`mysqlx::*`、X Protocol専用)のヘッダーしか含まれておらず、classic APIは提供されていない(Oracleが近年のリリースでclassic APIを削除している)。

さらに、X DevAPIはX Protocol(既定ポート33060)専用だが、このプロジェクトの`docker-compose.yaml`はclassic protocol(`:3306`→`:13306`)しか公開しておらず、**X DevAPIでは接続すら成立しない**ことが実機検証で判明した。

**対応**: `libmysqlclient`(classic C API、`mysql.h`)を採用し、RAII(`std::unique_ptr`+カスタムデリータ)で包む設計に変更した。副次的な利点として、将来のC実装も同じ`libmysqlclient`を使うことになるため、「同じCライブラリをC++がRAIIでどう安全に包むか」という、当初想定していたより一段クリーンな比較教材になっている。

### 2. 依存関係管理はvcpkgではなくHomebrewを採用

vcpkgでのBoost/MySQLビルドは(特にソースビルドの場合)長時間かかることが見込まれたため、macOS上ではプリビルド済みのHomebrewパッケージ(`brew install cmake boost mysql-client nlohmann-json googletest`)を採用した。CI環境やLinuxでの再現性が必要になった場合はvcpkgへの移行を検討する。

## アーキテクチャ

```
Router (http/router.cpp)
   │  method + path でHandlerへ振り分け(汎用ルーティングライブラリは使わない)
   ▼
TaskHandler (application/task_handler.cpp)
   │  バリデーション・JSON変換
   │  co_await db::RunBlocking(db_pool, [...]{ return repo.XXX(...); })
   ▼
TaskRepository (repository/task_repository.cpp)
   │  生SQL(Prepared Statement)。ORM禁止
   ▼
ConnectionPool (db/connection_pool.cpp)
   │  RAIIリース、libmysqlclientをラップ
   ▼
MySQL
```

### 同期DBアクセスの隔離(このアーキテクチャの核)

`libmysqlclient`は同期(ブロッキング)APIのため、HTTP用の`io_context`のスレッドで直接呼ぶと、そのスレッドが他の全接続の処理を止めてしまう。

対策として、DB呼び出し専用の`boost::asio::thread_pool`(`db_pool`、コネクションプールと同数=10)を用意し、`db/blocking.hpp`の`RunBlocking`ヘルパーで`asio::co_spawn(db_pool, ..., asio::use_awaitable)`によるexecutor間ブリッジを行っている。

```cpp
auto result = co_await db::RunBlocking(db_pool_, [this, id, user_id] {
    return repo_.FindById(id, user_id);  // ブロッキングでよい、db_poolのスレッドで実行される
});
```

`co_await`の間、呼び出し元のio_contextスレッドは解放され、他の接続の処理に使われる。実機検証(30並行リクエスト)で、応答が極端にブロックされないことを確認済み(下記「動作確認」参照)。

### RAIIによる所有権管理(Cとの比較ポイント)

- `db::ConnectionPool::Lease`: デストラクタで自動的にコネクションをプールへ返却する(Cなら`pool_release()`を全returnパスで手動記述する必要がある)
- `MysqlConnPtr`(`std::unique_ptr<MYSQL, MysqlDeleter>`): `mysql_close()`をデストラクタに紐付け、生成/破棄のペア忘れを構造的に防ぐ
- エラー処理は`std::expected<T, AppError>`で統一し、Repository層で`sql::Error`相当の例外をcatchして変換する境界を1箇所に固定している(Handler層は例外を一切見ない)

### ORM禁止

Repositoryで生SQL(Prepared Statement)を明示的に書いている。Rails以外の全言語(Go/Rust/Scala×2/JS/TS)と同じ方針で、言語比較なのかORM比較なのかが曖昧にならないようにしている。

### migrationsディレクトリは持たない

スキーマ正本は`backend/migrations`のみ。このC++実装も既存の`tasks`/`task_labels`/`labels`/`users`テーブルをそのまま読み書きする。

## セットアップ

```sh
# 依存関係(Homebrew、初回のみ)
brew install cmake boost mysql-client nlohmann-json googletest

# ビルド
cd backend-cpp
mkdir -p build && cd build
cmake ..
cmake --build . -j 4

# 起動(docker composeでmysqlが起動済みであること)
HTTP_ADDR=8105 ./backend_cpp_server
```

## 動作確認

- `cmake --build .`: 通ることを確認済み(AppleClang 21、C++23)。`-DENABLE_ASAN=ON`でAddressSanitizer/UndefinedBehaviorSanitizer付きビルドも可能(下記「結合テスト」参照)
- `ctest`(GoogleTest、DB接続不要な3実行ファイル): 全てpass。`backend_cpp_tests`4件(ステータス変換・日付バリデーション・UTF-8コードポイント長・UTC今日判定)、`backend_cpp_jwt_tests`6件(ローカルHMAC検証・Dispatcherのissuerルーティング/未知issuer拒否等)、`backend_cpp_jwks_tests`4件(自プロセス内にモックJWKSサーバーを立て、RS256の実署名検証・未知kid拒否・issuer不一致拒否・改ざん署名拒否を確認)
- 実機(docker-compose上のMySQL)に対して、以下を`curl`で実際に確認済み:
  - `GET /internal/v1/tasks`: 既存データ(実データ)を正しいJSON形状で返す
  - `POST /internal/v1/tasks`: 作成→201、日本語ラベルも含め正しく返る
  - `PATCH /internal/v1/tasks/{id}`: 更新→200、`label_ids: [1,1,3]`(重複)が`[1,3]`の2件に正規化されることを確認
  - `DELETE /internal/v1/tasks/{id}`: 削除→204、再削除→404(冪等性)、`task_labels`に孤立行が残らないことをSQLで直接確認(トランザクション保護が機能している)
  - バリデーション: 名前21文字→422、過去日付→422
  - 30並行リクエストが155ms程度で完了(同期DBアクセスの隔離が機能していることの簡易確認)

## 結合テスト

実DB(docker-compose上のMySQL)に接続する4つの結合テスト実行ファイルは、`ctest`のデフォルト対象には含めない(`add_test()`未登録。`ctest`実行のたびにdocker composeを要求しないため)。いずれもGoogleTestで書かれており、`cmake --build`が生成する実行ファイルを直接起動する。

```sh
docker compose up -d --wait mysql   # bff-gin/直下で実行
cmake --build build -j 4
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build/backend_cpp_repository_integration_tests
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build/backend_cpp_grpc_integration_tests
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build/backend_cpp_feature_flag_poller_tests
DB_HOST=127.0.0.1 DB_PORT=13306 DB_USER=root DB_SCHEMA=bff_gin_development ./build/backend_cpp_external_integration_tests
```

AddressSanitizer/UndefinedBehaviorSanitizer付きビルドで実行する場合は、あらかじめ`ASAN_OPTIONS=detect_container_overflow=0`を設定する(下記「ASanでのcontainer-overflow誤検知」参照)。

### Repository(`tests/task_repository_integration_test.cpp`)

`ConnectionPool`+`TaskRepository`を直接使い、HTTP/gRPC層を経由せずに検証する。テストごとに一意なemail/keycloak_subでユーザーを作り(`tests/db_fixture.{hpp,cpp}`の`TestUser`/`TestLabel`、デストラクタで自動的に後始末する)、共有の開発用DBを汚さない。カバー範囲: create/find/update/delete一連の往復、delete時の`task_labels`孤立行防止(トランザクション保護)、重複label_idの正規化、他ユーザーのtaskへの不可視性(所有権分離)、JWT認証のuser_id解決に使う`FindUserById`/`FindUserIdByKeycloakSub`。実機で全6件のpassを確認済み。

### gRPC(`tests/grpc_integration_test.cpp`)

`main.cpp`と同じ構成(実`TaskGrpcServiceImpl`+実`Dispatcher`)をテスト用ポート(`:19099`)で起動し、生成された`task::v1::TaskService::Stub`から実際にRPCを送る(C言語実装のようにワイヤーフォーマットを自作する必要はなく、gRPC C++の生成コードをそのまま使える)。実際に署名したローカルHMAC JWT(`tests/test_token_helper.{hpp,cpp}`の`MakeHmacToken`)を`authorization`メタデータとして送る。カバー範囲: create/get/update/delete一連の往復、delete時の`task_labels`孤立行防止、`authorization`メタデータ無しの呼び出しが`UNAUTHENTICATED`になること、期限切れJWTでの呼び出しが`UNAUTHENTICATED`になること、idのkeyset cursorによるページネーション(2ページ目・「次ページ無し」判定)。**この設計の既知の挙動**として、`next_cursor`は1件でも返せば必ず最後の行のidになる(次ページの有無を先読みしない簡略設計、`TaskRepository::ListCursorExternal`参照)ため、「次ページ無し」を確認するにはそのカーソルで再度呼んで0件が返ることまで確認する必要がある。実機で全5件のpassを確認済み。

### Feature Flagポーリング(`tests/feature_flag_poller_test.cpp`)

実DBに一意なテスト専用`flag_key`の行を挿入し、`FeatureFlagPoller::Start()`直後の即時ポーリング(10秒間隔を待たない)後に`Variation()`のフォールバック規則(enabled=true→default_variation、enabled=false→呼び出し側のdefault_value、flag_key未登録→同じくdefault_value)を検証する。他機能が参照する既存のflag行(`backend.external-tasks-pagination-v2`等)には一切触れない。実機で全3件のpassを確認済み。

### 外部公開API(`tests/external_handler_integration_test.cpp`)

実DB・モックJWKSサーバー(`tests/mock_jwks_server.{hpp,cpp}`、Boost::Beastで自プロセス内に構築しKeycloak相当として扱う)・外部公開API自身のリスナー(`Router`+`RunListener`をテスト用ポート`:18110`で起動)の3つを1プロセス内に立て、`tests/http_test_client.{hpp,cpp}`で実際にHTTP GETを送ってエンドツーエンドに検証する。カバー範囲: `Authorization`ヘッダ無し→401、`azp`不一致のトークン→401、正しく署名されたローカルHMACトークン(issがローカルという理由だけで拒否、Client Credentials Grant以外は受け付けない)→401、`user_id`欠如→400、v1(offset)ページングがページ境界をまたいで正しく動くこと(`page`/`page_size`/`total`)、v2(cursor)ページングが`next_cursor`を正しく連鎖させ最終ページで空配列になること。`backend.external-tasks-pagination-v2`は退避→書き換え→テスト後に復元する(全言語で共有する1つのFeature Flagのため、他のテスト・実機動作に影響を残さない)。実機で全6件のpassを確認済み。

### ASanでのcontainer-overflow誤検知

`backend_cpp_grpc_integration_tests`・`backend_cpp_external_integration_tests`をASanビルドで実行すると、`label_ids`(protobufの`RepeatedField`)を反復する箇所で`container-overflow`が報告される。これはHomebrew配布のプリビルド`libprotobuf`/`grpc++`がASanのコンテナアノテーション無しでビルドされているために起きる既知の誤検知(protobufのRepeatedFieldとASanのコンテナオーバーフロー検出機構の既知の組み合わせ問題)で、自プロジェクトのコードには該当箇所に実際のメモリ安全性の問題は無い。`ASAN_OPTIONS=detect_container_overflow=0`を設定することで、ヒープ/スタックバッファオーバーフローやuse-after-free等の他の検出機能はそのまま有効にしつつ、この既知の誤検知だけを抑制できる。

## JWT/JWKS認証(3issuer)

`src/auth/jwt.{hpp,cpp}`に、backend(Go)・backend-rustと同じ2段構造(issで担当Verifierへ振り分けるDispatcher)を実装した。

- **HmacVerifier**: ローカルHMAC発行(`iss=bff-gin-local-hmac`)、HS256。共有シークレットで`HMAC(EVP_sha256)`を計算し`CRYPTO_memcmp`で比較する
- **JwksVerifier**: Keycloak発行・ローカルRSA発行(`iss=bff-gin-local-rsa`)共通、RS256。JWKSのn/eから`EVP_PKEY_fromdata`(OpenSSL 3 API)でRSA公開鍵を組み立て、`EVP_DigestVerify`で検証する。kid不一致時のみJWKS再取得する戦略も他言語と同じ

**JWTライブラリの選定**: `jwt-cpp`はHomebrewに存在しない(`brew install jwt-cpp`は失敗する)。ヘッダオンリーで手動導入する選択肢もあったが、このプロジェクトの「フレームワーク任せにせず低レイヤーを見せる」という方針(README.md冒頭の「アーキテクチャ選定」参照)に合わせ、**OpenSSL(HMAC/EVP API)を直接使い、JWTのbase64url decode・署名検証を自前で実装する**方針にした。副次的に、C++がbase64・HMAC・RSA検証という「地味だが重要な低レイヤー処理」をどう安全に(RAII・`std::expected`で)書くかという、追加の学習教材になっている。

user_id解決(`ResolveUserId`)は、ローカル発行issuerなら`sub`をそのまま`users.id`として、Keycloak発行issuerなら`sub`(keycloak_sub)を`user_keycloaks`テーブル経由で引く(backend-rustの`resolve_user_id`と同じ設計)。Authorizationヘッダが無い場合のみ`X-Debug-User-Id`ヘッダにフォールバックする(単体動作確認用)。

## 外部公開API(:8109)とFeature Flagポーリング

- `src/flags/feature_flag_poller.{hpp,cpp}`: 自分専用のMySQL接続で`feature_flags`テーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、backend-rustの`src/flags.rs`と同じ設計)。バックグラウンド`std::thread`+`std::mutex`で保護した`std::map`キャッシュ
- `src/external/external_handler.{hpp,cpp}`: `GET /external/v1/tasks`。Client Credentials Grant(Keycloak発行、`azp`が`EXTERNAL_API_CLIENT_ID`と一致することを要求)のみ受け付ける。`backend.external-tasks-pagination-v2`で offset(v1: `page`/`page_size`)/cursor(v2: `cursor`/`limit`、id昇順のkeyset pagination)を切り替える
- **簡略化した点**: cursorはid単体(backend-rustはcreated_at+idの複合カーソル)。学習目的では十分だが、同一created_atのタイムスタンプが複数ある場合の厳密な安定ソートという観点では簡略化されている

## 動作確認(このフェーズ、実機で確認済み)

- 実際のローカルHMAC JWT(自作の署名検証コードで検証)でREST v1のCRUD一式を再確認: 未認証→401、認証あり→200/201/204、いずれも実データで確認
- 実際にKeycloakから`client_credentials`トークンを取得し、外部公開API(:8109)のoffset/cursor両モードを確認(`next_cursor`が正しく次ページの起点を指すことも確認)。確認後`backend.external-tasks-pagination-v2`は元に戻した
- 起動したサーバープロセスは全て停止し、テストデータも削除済み

## gRPC v2(:9099)

- `proto/task/v1/task.proto`: 他言語と全く同じ内容(`option go_package`のみ除去)
- CMakeのカスタムコマンドで`protoc`+`grpc_cpp_plugin`(ともにHomebrewの`grpc`/`protobuf`パッケージに同梱)を呼び出し、ビルド時に`task.pb.{h,cc}`・`task.grpc.pb.{h,cc}`を`build/generated/task/v1/`へ生成する(`.gitignore`の`build/`で除外済み)
- **`grpc::CallbackServerContext`+`ServerUnaryReactor`によるCallback API**でTaskServiceを実装(`src/grpc/task_grpc_service.{hpp,cpp}`)。古いCompletion Queueベースの同期/非同期APIは使っていない
- **gRPCのスレッドモデルについての設計判断**: RESTはBoost.Asioの`io_context`(非同期)上で動くため、ブロッキングなDB呼び出しは`db_thread_pool`へ明示的にディスパッチする必要があった(`db/blocking.hpp`)。gRPC C++のCallback APIは各RPCをgRPC自身が管理するスレッドプール上で呼び出すため、`io_context`とは完全に別の実行環境になる。このスレッドプール自体がブロッキング処理を想定した設計のため、Repositoryへの呼び出しはここでは`RunBlocking`へ包まず、そのまま直接ブロッキング呼び出しにしている(gRPC公式ドキュメントでも、Callback APIハンドラ内でのブロッキング処理自体は許容されている。極端に高い同時実行数が必要な場合のみ専用スレッドプールへのさらなるディスパッチが推奨される)
- **認証**: gRPCメタデータの`authorization`から、REST(`task_handler.cpp`)と共通の`application::ResolveUserIdFromAuthHeader`(`src/application/user_resolver.{hpp,cpp}`に切り出し)を呼ぶ。認証ロジックの複製は無い
- **Domain⇔protobufのMapper**: `src/grpc/task_mapper.{hpp,cpp}`の`ToProto()`に集約。MySQL DATETIME文字列(常にUTC)は`timegm`でUTC epoch秒に変換し`google::protobuf::Timestamp`にする(ローカルタイムゾーンに依存する`mktime`は使わない、CONTRACT.mdセクション23.1のUTC統一方針を踏襲)
- **Repository層はREST/外部API/gRPCで共有**: `TaskRepository::Delete`のトランザクション保護・`Create`/`Update`の重複label_id正規化はgRPC側でも自動的に効く(新たに実装したロジックはゼロ)。`ListTasks`は`ListCursorExternal`(idのみのkeyset cursor)を再利用しており、name/status/label_idsによる絞り込みは本フェーズでは未対応(REST v1と同じ簡略化)
- **エラー→gRPCステータス変換**: `AppErrorKind`→`grpc::StatusCode`(`kNotFound`→`NOT_FOUND`、`kUnauthorized`→`UNAUTHENTICATED`、`kDbError`→`INTERNAL`、バリデーション系→`INVALID_ARGUMENT`)
- **ログ**: 1rpc1行で`method`/実際の`grpc::Status::error_code()`/`duration_ms`を記録(他6言語と同じ精度、backend-rustの`logged()`と同じ設計)

### CMakeでのハマりどころ

- `find_package(Protobuf REQUIRED)`(legacyなModule mode)と、gRPCが内部で使う`protobuf-config.cmake`(Config mode)が混在すると`Some (but not all) targets in this export set were already defined`エラーになる → `find_package(Protobuf CONFIG REQUIRED)`で明示的にConfig modeに統一して解決
- `get_target_property(... gRPC::grpc_cpp_plugin LOCATION)`は信頼できず(非推奨、値が正しく取れないケースがある)、`$<TARGET_FILE:gRPC::grpc_cpp_plugin>`ジェネレータ式に置き換えて解決
- `protoc`は`-I`で指定したディレクトリからの相対パス構造を出力先にも反映する(`proto/task/v1/task.proto`→`生成先/task/v1/task.pb.cc`)。フラットな出力先を期待して`OUTPUT`を書くとファイルが見つからないエラーになる

## 動作確認(gRPC、実機で確認済み)

- 実際の`grpcurl`(`brew install grpcurl`)+ローカルHMAC JWTで、`ListTasks`(実データ・`next_cursor`の値まで確認)・`CreateTask`(`label_ids:["63","63"]`→1件に重複排除)・`GetTask`・`DeleteTask`→`GetTask`(削除後は`NotFound`)を一通り確認
- `task_labels`に孤立行が残っていないこと(`SELECT COUNT(*) FROM task_labels WHERE task_id=...`が0件)をSQLで直接確認、トランザクション保護が機能していることを確認
- 認証なしの呼び出しが`UNAUTHENTICATED`になることを確認
- 21文字の名前で`CreateTask`を呼ぶと`INVALID_ARGUMENT`になることを確認
- ログに`grpc method=list_tasks status=16 duration_ms=0`のように実際のステータスコード(16=UNAUTHENTICATED、0=OK、5=NOT_FOUND、3=INVALID_ARGUMENT)が記録されていることを確認
- gRPC実装後もREST v1(`:8105`)が引き続き200を返すことを確認(REST/gRPCが同じRepositoryを問題なく共有できている)
- 起動したサーバープロセスは停止済み、テストデータも削除済み

## ログについて

環境変数`LOG_LEVEL`(`debug`/`info`/`warn`/`error`、既定`info`、backend(Go)/bff/gateway(Go)/backend-c等と同じ規約)でデバッグ行の出力有無を切り替えられる。第三者ロギングライブラリは使わず、`src/common/logging.hpp`/`logging.cpp`の`LogDebug`が有効/無効を切り替えるだけの最小限の実装(backend-cの`src/common/log.h`/`log_debugf`のC++版、C側と同じく「デバッグ行を出すか否か」の1点のみを制御し、warn/errorレベル自体の出し分けは行わない)。`Config::FromEnv()`(`src/config.cpp`)がプロセス起動時に一度だけ`LOG_LEVEL`を読み、`main()`冒頭で`common::LogModuleInit(config.log_level)`を呼んで反映する

- **リクエスト単位のログ(INFO、既定で常に出る、`common::LogInfo`)**: REST v1・外部公開API・gRPCの3トランスポートいずれも、実際に送信したHTTPステータス/gRPCステータスをそのまま記録する(推測・再計算しない)
  - `rest method=<METHOD> path=<PATH> status=<実際のHTTPステータス> duration_ms=<経過ms>`: `Router::Dispatch`(`src/http/router.cpp`)。REST v1・外部公開APIは同じ`Router`実装を共有しており(1箇所の実装でルート追加時の書き漏れを防ぐ、backend-pythonのミドルウェアと同じ狙い)、`main.cpp`がインスタンスごとに渡す`log_module`("rest"/"external")で先頭語のみ出し分ける。`invalid_id`(400)・`not_found`(404)を含む全終了経路をカバーする
  - `external method=<METHOD> path=<PATH> status=<実際のHTTPステータス> duration_ms=<経過ms>`: 外部公開API用`Router`インスタンス(`main.cpp`で`http::Router external_router("external")`として構築)が上記と同じ`Router::Dispatch`実装で出力する
  - `grpc method=<gRPCメソッド名> status=<実際のgRPCステータスコード> duration_ms=<経過ms>`(`src/grpc/task_grpc_service.cpp`のLogRpc、既存)
- **DEBUG時のみ出る追加ログ(`LOG_LEVEL=debug`、`common::LogDebug`)**:
  - `auth debug: resolved user_id=<id> via local issuer=<iss>` / `auth debug: resolved user_id=<id> via keycloak_sub=<sub>`: REST/gRPC共通の`ResolveUserIdFromAuthHeader`(`src/application/user_resolver.cpp`)が認証成功時に解決した`user_id`
  - `auth debug: jwks refresh triggered url=<url> kid=<kid>`: `JwksVerifier::Verify`(`src/auth/jwt.cpp`)がkid不一致でJWKSを再取得するタイミング
  - `rest debug: list user_id=<id> limit=<limit> offset=<offset>`: REST v1の一覧取得(`TaskHandler::List`、`src/application/task_handler.cpp`)が解析した`limit`/`offset`
  - `external debug: list_offset user_id=<id> page=<page> page_size=<page_size>` / `external debug: list_cursor user_id=<id> after_id=<after_id> limit=<limit>`: 外部公開APIの一覧取得(`ExternalHandler::List`、`src/external/external_handler.cpp`)が解析したページング指定(offset方式/cursor方式)。`after_id`未指定時は`0`
- 実機で確認済み: `LOG_LEVEL`未設定(既定)で`GET /internal/v1/tasks`を呼んでも上記DEBUG行は一切出ないこと(INFOの1行サマリのみ出ること)、`LOG_LEVEL=debug`で起動して同じリクエストを呼ぶと`auth debug: resolved user_id=45 via local issuer=bff-gin-local-hmac` / `rest debug: list user_id=45 limit=3 offset=0`が実際に出力されることの両方を確認済み。またREST(200/401/404)・外部公開API(200/401)の複数パターンで、`rest method=GET path=/internal/v1/tasks status=200 duration_ms=13`・`external method=GET path=/external/v1/tasks status=200 duration_ms=19`のように実際のステータスコードがログに記録されることを確認済み

## 配線(bff/gateway/migration)

`backend.task-language`に`cpp`が追加済み(migration`000017`)
`bff`の`Clients`マップに`"cpp:rest"`/`"cpp:grpc"`が登録済み
`gateway/go`・`gateway/nginx/sidecar`の外部公開API振り分けにも`"cpp"`が登録済み
`backend.task-language`を`cpp`に切り替え、bff経由でREST/gRPC両方から実データが取得できることを実機確認済み(`docker compose exec mysql mysql -uroot bff_gin_development -e "UPDATE feature_flags SET default_variation='cpp' WHERE flag_key='backend.task-language';"`)
