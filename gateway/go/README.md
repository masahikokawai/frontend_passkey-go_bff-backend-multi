# gateway/go(外部公開APIゲートウェイ、Go製)

CONTRACT.mdセクション20.7・20.8参照
外部公開API(`/external/v1/tasks`、Client Credentials Grant認証、セクション11)は
元々backend自身が`:8081`で直接応答していたが、`backend.task-language`
フラグに応じてどの言語のbackendが応答するかを切り替えられるようにするため、
`:8081`をこのゲートウェイが引き継ぎ、backend自身は内部アドレス`:8097`へ後退した

## 重要な制約: 現状Goにしかフォールバックしない

セクション20.1〜20.6で実装したRust/Scala(http4s)/Scala(Pekko)/Railsは、いずれも
**内部CRUD(`/internal/v1/tasks`、bff経由)のみ**を実装しており、外部公開API
(`/external/v1/tasks`)はまだGoにしか存在しない
そのため`backend.task-language`が go 以外を指していても、このゲートウェイは**常にGoの外部公開API(`:8097`)へフォールバックする**
(`Gateway.resolveTarget`、bffの`TaskRoutes.pickClient`と全く同じ設計、警告ログ付き)
他言語の外部公開API実装は今回のスコープ外

## セットアップ

```sh
cd gateway/go
go mod download
```

## 実行

```sh
# training-go/bff-gin ルートで
docker compose up -d --wait mysql redis keycloak swagger-ui
cd backend && go run ./cmd/migrate up && go run ./cmd/server &   # REST:8090 gRPC:9090 外部API:8097

cd gateway/go
go run .
```

既定で`:8081`(既存の外部公開APIの公開ポートをそのまま引き継ぐ)で待ち受ける
`gateway/nginx`とはどちらか一方だけを起動すること(どちらも`:8081`をbindするため同時起動は不可)

## 環境変数

| 変数 | 既定値 | 説明 |
|---|---|---|
| `GATEWAY_ADDR` | `:8081` | このゲートウェイ自身の待受アドレス |
| `FEATURE_FLAG_EXPORT_URL` | `http://localhost:8090/internal/v1/feature-flags/export` | backendのexportエンドポイント(bffと同じ) |
| `FEATURE_FLAG_POLL_TOKEN` | `local-dev-feature-flag-poll-token` | bff/backendと同じ共有シークレット |
| `FEATURE_FLAG_POLL_INTERVAL_SECONDS` | `10` | ポーリング間隔(bffの既定値と合わせている) |
| `GATEWAY_GO_EXTERNAL_BASE_URL` | `http://localhost:8097` | backend(Go実装)の外部公開APIの接続先 |

## 動作確認

```sh
# Keycloakからexternal-api-clientのトークンを取得(CONTRACT.mdセクション11参照)
TOKEN=$(curl -s -X POST http://localhost:8082/realms/training/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=external-api-client -d client_secret=<realm-export.jsonの値> \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")

curl http://localhost:8081/external/v1/tasks?user_id=1 -H "Authorization: Bearer $TOKEN"
```

`:8097`(backend自身)を直接叩いた場合と同じレスポンスが返ることを確認する

`backend.task-language`をDBで`rust`等へ変更すると、ゲートウェイのログに
`backend.task-languageが未実装の言語を指しているためgoへフォールバック`という警告が出て、
それでも`:8097`(go)へ転送された応答が返り続けることを確認できる

## テスト

```sh
go test ./... -v
```

10件、DB接続不要ですべてpassする(`resolveTarget`のフォールバックロジック、
httptestによる実際のリバースプロキシ転送、`LanguageResolver`の実ポーリング・パース、
`Load()`の既定値・env override)

## Go製リバースプロキシ vs nginx+サイドカー方式、実装してみての所感

- **Goで書く方が「今どの言語か」を直接コード内で持てて分かりやすい**:
  - `httputil.ReverseProxy`の`Rewrite`フックにflag評価をそのまま埋め込めるため、リクエストごとに最新の状態を反映できる
  - bffの`pickClient`と全く同じ設計をそのまま持ち込めたのは、両方とも「flagを評価してinterface越しに振り分ける」という同じ形をしているため
  - TaskBackendClient ⇔ http.Handler 程度の違いしかない
- **nginx版は「設定ファイル+リロード」という別の現実的パターンを学べる**:
  - nginx 自体は DB の Feature Flag のような動的な状態を評価できないため、外部プロセス(サイドカー)が定期的に「現在あるべき設定」を計算し、
  - ファイルへ書き出してから`nginx -s reload`で反映させる、という実務のインフラでもよく見る構成になる
  - Go版が「即座に反映」なのに対し、nginx版は「ポーリング間隔 + reload 完了までのタイムラグ」がある点が明確な違いとして体感できた
- **責務の分離という観点では、むしろnginx版の方が「筋が良い」場面もありうる**:
  - nginx はリバースプロキシとしての実績・性能・機能(TLS終端、レート制限等)が豊富にあり、「ルーティング判断(サイドカー)」と「実際のプロキシ処理(nginx)」を分離できる
  - 学習用の小さな構成では Go 版の方が理解しやすいが、実務で nginx/Envoy 等が好まれる理由の一端が体感できた
