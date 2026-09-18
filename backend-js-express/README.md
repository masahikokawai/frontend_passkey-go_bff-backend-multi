# backend-js-express

CONTRACT.mdセクション20の多言語backend比較(Go/Rust/Scala(http4s)/Scala(Pekko)/Rails)に、
**JavaScript実装**として追加した6番目のTask CRUD backend。

`backend-rust`(GoのようなORM比較機能・migrationを持たず、Task CRUDのREST v1+gRPC v2+外部公開APIのみを
実装した、最も素直な「対等な1言語分」の実装)を主な参考実装とし、同じワイヤー契約・同じ既知バグの回帰防止を
JavaScript(プレーン、TypeScriptは`backend-js-ts-express`で別途追加予定)で再現している。

## 起動方法

```sh
# 依存パッケージのインストール(初回のみ)
cd backend-js-express
npm install

# 起動(REST v1 / gRPC v2 / 外部公開APIの3つを同時に立てる)
npm start
# REST v1:          http://localhost:8103
# gRPC v2:          localhost:9097
# 外部公開API:      http://localhost:8107(gateway経由でアクセスする場合は:8081)
```

事前に`docker compose up -d --wait mysql redis keycloak swagger-ui`(bff-gin直下)でMySQL/Keycloakが
起動していること、`cd backend && go run ./cmd/migrate up`でスキーマが適用済みであることが前提
(このディレクトリ自体はmigrationを持たない。スキーマの正本は`backend/migrations`のみ)。

## 単体テスト

```sh
npm test
```

Node.js標準の`node:test`を使用(追加の依存を増やさないため、Jest/Mocha等は導入していない)。

## ポート割り当て

| 用途 | ポート | 環境変数 |
|---|---|---|
| 内部REST v1 | `:8103` | `HTTP_ADDR` |
| 内部gRPC v2 | `:9097` | `GRPC_ADDR` |
| 外部公開API(内部アドレス) | `:8107` | `EXTERNAL_HTTP_ADDR` |

他言語(Go:8090/9090/8097、Rust:8093/9093/8098、Scala(http4s):8094/9094/8099、
Scala(Pekko):8095/9095/8100、Rails:8096/9096/8101)・gateway(:8081)・bff-rails(:8102)と
衝突しない値を選んでいる。

## 設計判断・技術選定

- **Express**: 最も標準的で学習コストが低いHTTPフレームワークのため採用(Fastifyも検討したが、
  この比較プロジェクトの主題は「言語間のワイヤー契約パリティ」であり、フレームワーク自体の性能比較は
  対象外のため、知名度・資料の多さを優先した)
- **プレーンJavaScript(CommonJS)**: TypeScript版(`backend-js-ts-express`)は別ディレクトリで追加予定。
  この2つを可能な限り「型注釈の有無だけが違う」双子にすることで、CONTRACT.mdの検証観点
  「静的型付けの効果を単体で切り出して比較する」という目的に沿わせている。CommonJSを選んだのは、
  Node.js標準の`node:test`ランナーや`require`ベースの依存関係がこのプロジェクトの他のNode.js
  利用箇所(frontend、e2eのJS勢)と混同されないよう、意図的に独立したモジュールシステムのまま
  シンプルに保つため(ESMでも成立するが、追加のビルド設定は不要にしたかった)
- **MySQLの日時列は文字列のまま扱う(`mysql2`の`dateStrings: true`)**: DATE/DATETIME列をJSの`Date`
  オブジェクトへ変換すると、Node実行環境のローカルタイムゾーンが暗黙に介在してしまう。これは
  CONTRACT.mdセクション23.1に記録されている「`finished_on`の過去日判定がタイムゾーンでずれる」
  既知バグ(Go・Scala×2はローカルタイムゾーン基準、Rust・RailsはUTC基準で割れていた)と全く同じ
  クラスの問題を生みかねないため、この実装は最初から「日時は常にUTC基準の文字列」という設計にして
  同種のバグが構造的に発生しない形にしている
- **JWT/JWKS検証を自前実装**: `jwks-rsa`のようなラッパーライブラリに頼らず、`jsonwebtoken` +
  Node標準の`crypto.createPublicKey({format:'jwk'})`で直接実装した。理由は、他言語実装
  (backend-rustの`JwksVerifier`等)と同じ「kid不一致時のみ再取得」というキャッシュ戦略を明示的に
  再現し、ブラックボックスに頼らず学習効果を保つため
- **gRPCのidフィールドは`longs: Number`で読む**: `@grpc/proto-loader`の既定ではuint64は`Long`
  オブジェクトになるが、このプロジェクトの規模(学習用途、`Number.MAX_SAFE_INTEGER`を超えるIDは
  発生しない)ではJS Numberとして扱う方が呼び出し側のコードが素直になるため単純化した

## backend-rustとの既知の相違点

- **`deleteTask`でtask_labelsも削除する**: `task_labels`テーブルには外部キー制約が無い
  (`backend/migrations/000004`、ON DELETE CASCADE無し)。backend(Go)は
  `internal/repository/task.go`の`Delete`で「タスク削除と同じトランザクションで
  `task_labels`の関連行も削除する」よう修正済みだが、**backend-rustの`db.rs::delete_task`は
  この後始末を欠いており、タスク削除後にtask_labelsの孤立行が残る**(既存の実装差異、
  今回の作業で発見)。本実装はGoの修正済みの挙動に合わせ、タスク削除と同じトランザクションで
  `task_labels`も削除している

## 未確認・今後の課題

- gRPC v2・外部公開APIはコードレビューベースでCONTRACT.mdの契約に準拠させたが、REST v1ほど
  網羅的な実機確認は行えていない(README作成時点の状況。詳細は作業報告を参照)
- bff・admin/go・admin/rails・gatewayへの配線(`backend.task-language`への`javascript`追加)は
  別フェーズの作業であり、本ディレクトリの範囲外
