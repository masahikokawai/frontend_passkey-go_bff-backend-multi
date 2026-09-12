# backend-scala-pekko

CONTRACT.mdセクション20: backendのTask CRUDを、Go実装(`backend/`)と全く同じワイヤー契約
(REST v1のJSON形状・ステータスコード・エラーボディ、gRPC v2の`.proto`・ステータスコード)を保った
まま、Scala(Pekko HTTP + pekko-grpc)で比較実装したもの。

**Task CRUDのみが対象。** Label CRUD・User管理・Feature Flag管理APIはGo実装のみに残る
(CONTRACT.mdセクション20.1)。

【追記】CONTRACT.mdセクション11・20.7: BFF非経由の外部公開API(`/external/v1/tasks`、
Client Credentials Grant認証)も追加実装済み(下記「外部公開API」節参照)。

## 最重要: ライセンス方針(Akkaを使わない)

2022年、AkkaはApache 2.0からBSL(Business Source License、商用利用に制限がある
ライセンス)へ変更された。本プロジェクトは学習用OSSであり、この制約を避けるため、
**`org.apache.pekko`(Akkaのコミュニティフォーク、Apache 2.0を維持)のみを使用し、
`com.typesafe.akka`配下のartifactは一切使わない。** gRPCも`pekko-grpc`(`akka-grpc`の
Apache 2.0継続フォーク)を使う。`build.sbt`冒頭にもこの方針をコメントで明記している。

`backend-scala-http4s`(Typelevelスタック: cats-effect + http4s + doobie + circe)との
対比として、本実装はあえて**伝統的なPekko/Akkaエコシステムの組み合わせ**
(Slick + spray-json)を選んでいる。

## 技術選定

| 関心事 | 採用 | 備考 |
|---|---|---|
| REST | Pekko HTTP(Route DSL) | |
| gRPC | pekko-grpc(`server_power_apis`生成設定) | Metadataからauthorizationヘッダを取得するため |
| DB | Slick + slick-hikaricp | http4s実装のdoobieとの対比としてあえてSlickを選択 |
| JSON | spray-json | http4s実装のcirceとの対比としてあえてspray-jsonを選択(手書きの`RootJsonFormat`でsnake_caseキーに合わせている) |
| JWT検証 | nimbus-jose-jwt(Java) | `RemoteJWKSet`がkidベースのキャッシュ・再取得を標準で提供するため、Goのjwks.goのような自前キャッシュ実装が不要 |
| Actor | 未使用 | 単純なCRUDにActorモデルを持ち込む必然性が無いため、Route DSLで素直に実装した |

## セットアップ

```sh
cd backend-scala-pekko
sbt compile
```

初回は`.proto`からのコード生成(pekko-grpc)とライブラリ解決が走るため時間がかかる。

## 実行

```sh
# training-go/bff-gin ルートで
docker compose up -d --wait mysql

cd backend-scala-pekko
sbt run
```

既定でREST(`:8095`)・gRPC(`:9095`)の両方が同一プロセス内で起動する(Goの`cmd/server/main.go`が
1プロセスでGin/gRPCを両方起動しているのと同じ構成)。環境変数はGo実装(`backend/internal/config/config.go`)
と同じ命名規則・既定値を踏襲している(`KEYCLOAK_ISSUER`・`EXPECTED_AUDIENCE`・
`LOCAL_AUTH_HMAC_SECRET`・`LOCAL_AUTH_RSA_JWKS_URL`等)。DB接続だけは`DB_DSN`という単一文字列
ではなく`DB_HOST`/`DB_PORT`/`DB_NAME`に分けている(JDBC URLの組み立て上の都合。ワイヤー契約には
影響しない既知の差異)。

## 外部公開API(BFF非経由、Client Credentials Grant、CONTRACT.mdセクション11・20.7)

内部CRUDとは別に、`GET /external/v1/tasks`を`:8100`(`EXTERNAL_HTTP_ADDR`既定値)で追加提供する。

- **認証**: `Authorization: Bearer <token>`をKeycloak JWKSで検証し、`azp`クレームが
  `EXTERNAL_API_CLIENT_ID`(既定`external-api-client`)と一致することを追加確認する
  (`backend/internal/authjwt/external_middleware.go`のRequireExternalClientAuthに対応)
- **ページネーション**: `backend.external-tasks-pagination-v2`というFeature Flagで
  offset(`page`/`page_size`/`total`)/keyset cursor(`cursor`/`limit`/`next_cursor`)を切り替える。
  **このflagは5言語(Go/Rust/Scala×2/Rails)で共有する1つのflag**であり、Scala(Pekko)自身が
  `feature_flags`テーブルを直接ポーリング(10秒間隔、`FeatureFlagPoller`)して評価する
  (bff/gatewayのようなHTTPポーリングではなく、backend自身がDBを正本として直接読むGo実装と同じ設計)
- レスポンスのTask JSONは内部CRUDと異なり**`user_id`を含まない**(Go実装の
  `internal/handler/external/task.go`のtaskDTOToJSONと同じ)
- cursorは`base64url("<RFC3339Nano相当>|<id>")`の不透明文字列(`ExternalCursorCodec`)

## テスト

```sh
sbt test
```

- `ModelSpec`: `TaskStatus`のRails enum互換マッピング
- `TaskServiceSpec`: バリデーション(name必須・20文字制限・過去日禁止・status妥当性)、
  `resolveUserId`(ローカル発行/Keycloak発行issuerの分岐、未プロビジョニング判定)、
  認可(他人のtaskへのUpdateがNotFound扱いになること)を、実DBを使わない
  `InMemoryTaskRepository`/`InMemoryUserRepository`で検証
- `TaskRoutesSpec`: `pekko-http-testkit`でREST層のステータスコード・エラーボディ契約
  (401 unauthorized/invalid_token、403 user_not_provisioned、422 invalid_status/
  invalid_finished_on/validation_error、400 invalid_request、404 not_found、
  201/200/204の正常系)を検証
- `FeatureFlagLogicSpec`: `backend.external-tasks-pagination-v2`の評価規則(enabled/
  default_variation/variationsの組み合わせ)がGo実装のBuildFlagConfigJSONと同じであることを検証
- `ExternalCursorCodecSpec`: cursorのencode/decode往復・不正値の扱い
- `ExternalTaskRoutesSpec`: 外部公開APIのステータスコード・エラーボディ契約
  (401 unauthorized/invalid_token、403 client_not_allowed、400 user_id関連、
  422 invalid_cursor、flag ON/OFFでのレスポンス形状切り替え、user_idを含まないことの確認)

## Go実装/http4s実装との既知の差異

- N+1の意図的再現(Go v1旧実装の教材)は再現していない。REST/gRPCとも効率的なJOINで実装
- DB接続設定の環境変数名(`DB_HOST`/`DB_PORT`/`DB_NAME` vs Goの`DB_DSN`)
- ログ形式: GoのJSON構造化ログ(slog)に対し、本実装はlogback(デフォルト設定のまま)。
  厳密な構造化ログ比較はスコープ外とした

## 実機検証で判明した点(Go実装のマイグレーションの見落とし)

当初、`backend/migrations/000001_create_users.up.sql`だけを見て`users`テーブルに
`keycloak_sub`カラムがあるものとして実装したが、実際にライブのMySQLへ接続してテストした際に
`Unknown column 'keycloak_sub' in 'field list'`で失敗した。原因は`000008_split_user_credentials.up.sql`
で`keycloak_sub`が`user_keycloaks`テーブル(`user_id`で`users`と1:1)へ切り出され、
`users`からは削除されていたため(CONTRACT.mdセクション16.2)。`Db.scala`の`UsersTable`から
`keycloakSub`列を削除し、`UserKeycloaksTable`を新設して`getByKeycloakSub`をjoinクエリに
修正した。マイグレーション履歴の後半まで見ないと現在のスキーマの実態を見誤る、という
分かりやすい教訓になった。

## 実装してみての所感

- **nimbus-jose-jwtのRemoteJWKSetは強力**: Go実装の`jwks.go`は「kidごとの公開鍵キャッシュ・
  未知kid時の再取得」を100行近く自前実装しているが、nimbus-jose-jwtでは`RemoteJWKSet`+
  `JWSVerificationKeySelector`の組み合わせだけでこれを標準サポートしている。JVMエコシステムの
  「枯れた大きめのライブラリに乗る」文化と、Goの「小さな標準ライブラリを組み合わせる」文化の
  違いが最も分かりやすく出た箇所
- **pekko-grpcの`server_power_apis`は必須級**: 素の生成トレイトはリクエストのメタデータ
  (Authorizationヘッダ)にアクセスできない。`server_power_apis`生成設定を有効にして初めて
  `(request, metadata)`のシグネチャになる。Goのinterceptorパターン(認証済みclaimsをcontextに
  積んでhandlerへ渡す)と比べると、Pekko側は「1リクエストごとに自分でmetadataからヘッダを読んで
  検証する」形になり、認証ロジックの共通化はミドルウェアではなく単純な関数呼び出しの共有で行っている
- **h2c(TLS無しHTTP/2)の起動はコード側ではなく設定ファイル側**: `Http().newServerAt(...).bind(...)`
  を書くだけでは起動せず、`application.conf`に`pekko.http.server.preview.enable-http2 = on`を
  置く必要がある。Goの`grpc.NewServer()`が何の設定も無くinsecureで即座に動くのと対照的で、
  「まだpreview機能である」ことがコード上にも表れている
- **spray-jsonの手書きフォーマットはボイラープレートが多い**: レスポンスのJSONキーをsnake_case
  (`finished_on`等)にするため`jsonFormatN`マクロを使わず`RootJsonFormat`を手書きした。circe
  (http4s実装側)なら`@JsonKey`アノテーション1つで済むところ、spray-jsonでは全フィールドを
  手で列挙する必要があり、この点は明確にcirceの方が書きやすい
- **Slickのクエリ合成はGORMの`db.Where(...)`チェーンに近い体験**: `var q: Query[...] = ...;
  q = q.filter(...)`という可変var方式でフィルタを段階的に組み立てる書き方は、型安全性こそ
  Slickの方が高いものの、書き味自体はGoのGORMコードとよく似ていた(doobieの生SQL+補間の方が
  Go実装との対比としてはむしろ遠い)
