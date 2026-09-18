# backend-scala-http4s

CONTRACT.md セクション20(backend多言語実装比較)のステップ4: Task CRUD の Scala(http4s + fs2-grpc)実装。
既存Go実装(`backend/`)と同一のワイヤー契約(REST v1のJSON形状・ステータスコード、gRPC v2の`.proto`・ステータスコード)
を持ち、同じMySQLの`tasks`/`labels`/`task_labels`/`users`/`user_keycloaks`テーブルへ接続する
(独自マイグレーションは持たない。スキーマの正本は`backend/migrations`のgolang-migrate)。

Label CRUD・User管理・Feature Flag管理APIはこの実装の対象外(引き続きGoのみ)。Task CRUDのみを実装している。

## セットアップ

Scala/sbtのツールチェーンが必要(初回、Homebrewの場合):

```sh
brew install openjdk sbt
```

```sh
cd backend-scala-http4s
sbt compile
```

## 起動

```sh
# frontend_passkey-go_bff-backend-multi/ ルートで
docker compose up -d --wait mysql redis keycloak

cd backend-scala-http4s
sbt run
```

既定でREST v1が`:8094`、gRPC v2が`:9094`で起動する(Go実装の`:8090`/`:9090`、Rust実装の`:8093`/`:9093`と
衝突しない値)。Goの`backend/cmd/server/main.go`が1プロセスでGin(REST)とgRPCを両方起動しているのと同じ構成で、
1つの`sbt run`プロセス内でhttp4s(Ember)サーバとfs2-grpc(Netty)サーバを両方起動し、DBコネクションプール
(HikariCP)とビジネスロジック層(`TaskService`)を共有する。

## 環境変数

Go実装(`backend/internal/config/config.go`)の命名規則・既定値をそのまま踏襲する。

| 変数 | 既定値 |
|---|---|
| `HTTP_ADDR` | `:8094` |
| `GRPC_ADDR` | `:9094` |
| `DB_DSN` | `root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true` |
| `KEYCLOAK_ISSUER` | `http://localhost:8082/realms/training` |
| `KEYCLOAK_JWKS_URL` | `${KEYCLOAK_ISSUER}/protocol/openid-connect/certs` |
| `EXPECTED_AUDIENCE` | `backend` |
| `LOCAL_AUTH_HMAC_SECRET` | `local-dev-hmac-shared-secret-change-me` |
| `LOCAL_AUTH_RSA_JWKS_URL` | `http://localhost:8080/.well-known/jwks.json` (bffのポート) |
| `EXTERNAL_HTTP_ADDR` | `:8099`(外部公開API専用、下記参照) |
| `EXTERNAL_API_CLIENT_ID` | `external-api-client` |

## 外部公開API(CONTRACT.mdセクション11・20.7、BFF非経由)

内部CRUD(`:8094`)とは別に、`:8099`で外部公開API(`GET /external/v1/tasks`)を同一プロセス内に
追加起動する。認証はKeycloakのClient Credentials Grantのみ(ローカルHMAC/RSAは対象外)で、
通常のJWT検証(`JwtAuth.verify`)に加えて`azp`クレームが`EXTERNAL_API_CLIENT_ID`と一致するかを
追加チェックする(`RequireExternalClientAuth`相当)。

ページネーション方式は`backend.external-tasks-pagination-v2`というFeature Flagで切り替わる。
**このflagは5言語(Go/Rust/Scala×2/Rails)すべてで共有する1つの値**であり(CONTRACT.mdセクション20.2)、
backend自身が`feature_flags`テーブルを10秒間隔で直接ポーリングして評価する(bffやgatewayのような
HTTP export経由のポーリングではない。Go実装の`featureflag.NewMySQLEvaluator`と同じ設計)。

- OFF(既定): offsetページング。`?user_id=&page=&page_size=` → `{"tasks":[...],"page":N,"page_size":N,"total":N}`
- ON: keyset(cursor)ページング。`?user_id=&cursor=&limit=` → `{"tasks":[...],"next_cursor":"..."|null,"limit":N}`
- レスポンスのTask JSON形状は内部CRUDと似ているが**`user_id`フィールドを含まない**点が異なる(Go実装と同じ)
- cursorはbase64url("<createdAtの文字列>|<id>")という不透明文字列。Go実装のcursor形式とバイト単位で
  一致させる必要はない(各backendが自分のcursorだけをdecodeできれば足りる設計のため)

実機確認: docker-compose上のMySQL・Keycloakを使い、`external-api-client`のClient Credentials Grantで
取得した実トークンで、flag ON(cursor、`limit`指定で複数ページに跨るcursor往復も含む)・OFF(offset)
両方の応答形状を確認済み。`user_id`欠落時は`{"error":"user_id is required"}`(400)、数値変換失敗時は
`{"error":"invalid user_id"}`(400、Go実装と文字列完全一致)、トークン無しは401、不正cursorは422で
それぞれ確認済み。

【注記】このflagは5言語で共有するため、他言語実装の並行動作確認(同じ開発DBの同じ行を操作する)と
同時にテストすると値が競合することがある(実際に本実装作業中も、並行して動いていた他言語フォークが
このflagを触ったことで一時的に予期しない応答形状になる場面があった。実装のバグではなく、共有flagを
複数プロセスが同時にポーリング・参照する設計上の性質)。

## 動作確認

```sh
sbt test
```

`src/test/scala/`に41件のユニットテストがある(バリデーション・ステータスコードマッピング(REST/gRPC/外部公開API)・
resolveUserID・HMAC JWT検証の正常系/異常系・外部公開APIのcursor encode/decode往復)。ローカルで実行し全件passすることを確認済み。

さらに、docker-compose上の実MySQL・実Keycloakに接続した状態で`sbt run`し、ローカルHMAC発行のJWT
(bffと同じ秘密鍵・issuer・audienceで自前生成したもの)を使って以下を実機確認済み:

- REST: Task一覧(空→作成→一覧反映→取得→更新→削除→一覧から消える)の一連のCRUD、`name`21文字での
  バリデーションエラー(422 `validation_error`)、存在しないIDでの404、未プロビジョニングユーザーでの
  403 `user_not_provisioned`
- gRPC(`grpcurl`使用): `CreateTask`/`ListTasks`/`GetTask`/`DeleteTask`、存在しないIDでの`NotFound`、
  不正トークンでの`Unauthenticated`、未プロビジョニングユーザーでの`PermissionDenied`(`user not provisioned`)

## 実装時に見つかった実際の不整合(Go実装との突き合わせで発覚)

着手前にGoのマイグレーション(`backend/migrations/000001_create_users.up.sql`)だけを読んで
`users`テーブルに`keycloak_sub`列があるものとして実装したが、実際にDBへ接続してテストしたところ
`Unknown column 'keycloak_sub' in 'field list'`で失敗した。原因は後続のマイグレーション
`000008_split_user_credentials`で`keycloak_sub`が`users`から`user_keycloaks`テーブル(`user_id`/`keycloak_sub`の
2列)へ分離されていたこと(CONTRACT.mdセクション16.2)。`backend/internal/repository/user.go`の
`GetByKeycloakSub`実装(`JOIN user_keycloaks`)を読み直し、`TaskRepo.findUserByKeycloakSub`もJOINするよう
修正して解決した。**「マイグレーションの最初の1ファイルだけでなく、最新状態(または実DBのDESCRIBE)を
確認する」ことの重要性を実機検証で再確認した**、という教訓をここに残す。

## http4s + fs2-grpc + cats-effect で実装してみての所感(Go実装との比較)

- **IOモナドによる副作用の明示**: Goでは`error`を戻り値として都度チェックする素朴なスタイルだが、
  cats-effectの`IO`はfor内包表記(`for { a <- ...; b <- ... } yield ...`)で逐次実行を記述でき、
  エラーは`raiseError`/`handleErrorWith`でモナディックに合成できる。Goの「呼び出し側が`if err != nil`を
  書き忘れるとコンパイルは通ってしまう」というリスクが構造的に無くなる一方、`IO`のどこで実際に
  実行されるか(遅延評価)を意識する必要があり、学習コストは相応にある。
- **型クラスベースの抽象化**: `Async[F]`のような型クラス境界を至る所で書く必要があり、Goの
  インターフェース(`TokenVerifier`等)よりも一段抽象度が高い。doobieの`ConnectionIO`とcats-effectの
  `IO`を`.transact(xa)`で繋ぐ設計は、GoのGORMがコネクションプールを暗黙に扱うのと対照的に、
  「DBトランザクションの型」が型システムに現れる点が興味深い。
- **ScalaPBの生成パッケージ名に注意が必要だった**: `.proto`の`package task.v1;`宣言に加えて、
  ファイル名(`task.proto`)がデフォルトでScalaパッケージ末尾に追加される(`task.v1.task`)ため、
  Goの`option go_package`のような一発指定に慣れていると、生成コードの実際の場所を
  `target/scala-2.13/src_managed`で確認しないとハマる(実際に本実装でも一度ハマった)。
  `option (scalapb.options).flat_package = true;`を`.proto`側に追記すれば`task.v1`のままにできるが、
  今回は既存の`.proto`ファイルを一切変更しない方針(CONTRACT.mdセクション20.5)のため、生成される
  パッケージ名の違いをScala側のimportで吸収する形にした。
- **grpc-netty-shadedのパッケージ相対配置**: 依存関係を`grpc-netty`ではなく`grpc-netty-shaded`にすると、
  Netty本体だけでなく`io.grpc.netty`パッケージ自体も`io.grpc.netty.shaded.io.grpc.netty`へ再配置される。
  「shaded」の再配置範囲がライブラリによって異なる(Nettyだけを隠す場合とgRPCのAPIパッケージごと
  隠す場合がある)ことを、コンパイルエラーを通じて実地で学んだ。
- **JWKS検証ライブラリはJava資産(nimbus-jose-jwt)に頼るのが安全**: Scala純正のJWTライブラリは
  RFC 7517(JWKS)対応が薄いものが多く、実績のあるJavaライブラリをそのまま使う方が結果的に堅牢だった。
  Goの`golang-jwt/jwt`が標準ライブラリの`crypto/rsa`と組み合わせて自前でJWK→RSA公開鍵変換をしている
  (backend/internal/authjwt/jwks.go参照)のに対し、nimbus-jose-jwtは`JWKSet.load(url)`一発で済み、
  この点はJava/Scalaのエコシステムの厚みが有利に働いた。
- **doobieのFragment合成**: 動的な絞り込み条件(name/status/label_ids)を`Fragments.in`等で安全に
  組み立てられ、SQLインジェクションのリスクなしに条件分岐をコードとして自然に書ける。GoのGORMの
  メソッドチェーン(`.Where(...).Where(...)`)と設計思想は近いが、doobieは値の型(`Long`/`String`等)が
  コンパイル時に効くため、プレースホルダの型ミスに気づきやすい。
