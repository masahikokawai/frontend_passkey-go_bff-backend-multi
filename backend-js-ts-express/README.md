# backend-js-ts-express

CONTRACT.mdセクション20の多言語backend比較(Go/Rust/Scala(http4s)/Scala(Pekko)/Rails/JavaScript)に、
**TypeScript実装**として追加した7番目のTask CRUD backend。

`backend-js-express`(同じくJavaScript実装)の**型付き双子**として作った。ロジック・API・HTTPフレームワーク・
エラーメッセージ・バリデーション仕様は`backend-js-express`と完全に同一で、違いは型注釈とコンパイル時チェックの
有無だけ。frontendの`frontend.tasks-ts-rewrite`(JS実装・TS実装を1:1でミラーする方針)と同じ考え方を
backendでも再現し、「静的型付けの効果だけを単独で切り出して比較する」ことを目的にしている。

## 起動方法

```sh
# 依存パッケージのインストール(初回のみ)
cd backend-js-ts-express
npm install

# 起動(REST v1 / gRPC v2 / 外部公開APIの3つを同時に立てる)
npm start
# REST v1:          http://localhost:8104
# gRPC v2:          localhost:9098
# 外部公開API:      http://localhost:8108(gateway経由でアクセスする場合は:8081)
```

事前に`docker compose up -d --wait mysql redis keycloak swagger-ui`(bff-gin直下)でMySQL/Keycloakが
起動していること、`cd backend && go run ./cmd/migrate up`でスキーマが適用済みであることが前提
(このディレクトリ自体はmigrationを持たない。スキーマの正本は`backend/migrations`のみ)。

## 型チェック・単体テスト

```sh
npm run typecheck   # tsc --noEmit(frontendのtsc --noEmitと同じ位置づけの独立した健全性チェック)
npm test            # tsx --test(Node標準のnode:testランナーをtsx経由でTypeScriptのまま実行)
```

## ビルド(参考、本番相当の起動をしたい場合)

```sh
npm run build        # tsc でdist/へコンパイル
node dist/main.js     # コンパイル済みJSを直接実行
```

## ポート割り当て

| 用途 | ポート | 環境変数 |
|---|---|---|
| 内部REST v1 | `:8104` | `HTTP_ADDR` |
| 内部gRPC v2 | `:9098` | `GRPC_ADDR` |
| 外部公開API(内部アドレス) | `:8108` | `EXTERNAL_HTTP_ADDR` |

他言語(Go:8090/9090/8097、Rust:8093/9093/8098、Scala(http4s):8094/9094/8099、
Scala(Pekko):8095/9095/8100、Rails:8096/9096/8101、backend-js-express:8103/9097/8107)・
gateway(:8081)・bff-rails(:8102)と衝突しない値を選んでいる。

## 設計判断・技術選定

- **backend-js-expressの型付き双子であること最優先**: 独自の設計判断は行わず、backend-js-expressのロジックを
  そのままTypeScriptへ移植した(型宣言・型ガードの追加のみ)
- **tsx + tsc --noEmitの二本立て**: 実行は`tsx`(esbuildベースのトランスパイルのみ、型チェックはしない)
  で行い、型チェックは`tsc --noEmit`で別途行う。これはfrontendが`npm run dev`(Vite/esbuild、型チェック無し)
  と`tsc --noEmit`(型チェックのみ)を分離しているのと同じ設計思想で、「実行を速く保ちつつ、型の健全性は
  別枠で保証する」という考え方を踏襲した
- **strict: true**: TypeScriptの型チェックの効果を最大化するため、`tsconfig.json`は`strict`を有効にしている
- **gRPCメッセージ型は手書き**: `@grpc/proto-loader`は実行時に動的に`.proto`を読み込む方式のため、
  静的な型定義を自動生成しない(ts-protoのようなコード生成ツールは、backend-js-expressの単純さを保つため
  意図的に導入していない)。そのため`src/grpc/messages.ts`に`.proto`のメッセージ形状を手書きの
  TypeScript interfaceとして宣言し、ハンドラのrequest/responseに型を与えている
- **mysql2の型**: `RowDataPacket`/`ResultSetHeader`等、mysql2が提供する型をそのまま利用している

## backend-js-expressとの相違点

無し(意図的に無くしている)。`src/`配下の各ファイルはbackend-js-expressの対応するファイルと1:1で対応し、
型注釈以外のロジック上の差分が生じていないことをレビューで確認している。

## backend-rustとの既知の相違点

backend-js-expressと同じ。`deleteTask`で`task_labels`も同一トランザクションで削除する(Goの修正済みの挙動に
合わせている。backend-rustの`db.rs::delete_task`はこの後始末を欠いており孤立行が残る、既知の差異)。

## 未確認・今後の課題

- gRPC v2・外部公開APIはbackend-js-expressと同様、REST v1ほど網羅的な実機確認は行えていない
- bff・admin/go・admin/rails・gatewayへの配線(`backend.task-language`への`typescript`追加)は
  別フェーズの作業であり、本ディレクトリの範囲外
