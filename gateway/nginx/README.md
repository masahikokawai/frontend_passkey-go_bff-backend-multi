# gateway/nginx(外部公開APIゲートウェイ、nginx製)

CONTRACT.mdセクション20.7・20.8参照
`gateway/go`(アプリケーションコード内で flag をポーリングして都度ルーティング先を決めるGo実装)との対比として、
nginx + サイドカー(設定ファイル書き換え + reload)という構成で同じ役割を実装したもの

## 重要な制約: 現状Goにしかフォールバックしない

`gateway/go` と全く同じ制約Rust/Scala(http4s)/Scala(Pekko)/Railsは外部公開APIをまだ実装していないため、
`backend.task-language`がgo以外を指していても、サイドカーは常に`go`(`:8097`)へ解決する(`resolveTarget`、警告ログ付き)

## セットアップ

```sh
brew install nginx
cd gateway/nginx/sidecar && go mod download
```

## 実行

```sh
# training-go/bff-gin ルートで
docker compose up -d --wait mysql redis keycloak swagger-ui
cd backend && go run ./cmd/migrate up && go run ./cmd/server &   # REST:8090 gRPC:9090 外部API:8097

cd gateway/nginx
./start.sh
```

`start.sh`が、nginx本体(`:8081`)とサイドカー(バックグラウンドの定期ポーリング)の両方を起動する
`Ctrl+C`で両方停止する(`trap`でnginxのstopまで面倒を見る)
`gateway/go`とはどちらか一方だけを起動すること(どちらも`:8081`をbindするため同時起動は不可)

手動でそれぞれ個別に動かしたい場合:

```sh
# nginx本体のみ
nginx -p "$(pwd)" -c nginx.conf         # 起動
nginx -p "$(pwd)" -c nginx.conf -t      # 設定検証のみ
nginx -p "$(pwd)" -c nginx.conf -s stop # 停止

# サイドカーのみ
cd sidecar && go run .
```

## 構成ファイル

- `nginx.conf`: システムのnginx(brew services)とは独立させるため、`-p`でこのディレクトリを
  prefixに指定して起動する前提(pid/ログ/一時ファイルは全てこのディレクトリ配下の相対パス)
- `upstream.conf`: サイドカーが自動的に書き換える`upstream`ブロック定義
  Gitには`backend.task-language`の既定値(`go`, `:8097`)に対応する内容をコミットしておく
- `sidecar/`: `backend.task-language`を定期ポーリングし、値が変われば`upstream.conf`を
  書き換えて`nginx -s reload`をトリガーするGoの小さなワンショット的常駐プログラム

## 環境変数(サイドカー)

| 変数 | 既定値 | 説明 |
|---|---|---|
| `FEATURE_FLAG_EXPORT_URL` | `http://localhost:8090/internal/v1/feature-flags/export` | backendのexportエンドポイント(gateway/goと同じ) |
| `FEATURE_FLAG_POLL_TOKEN` | `local-dev-feature-flag-poll-token` | 共有シークレット |
| `FEATURE_FLAG_POLL_INTERVAL_SECONDS` | `10` | ポーリング間隔 |
| `GATEWAY_GO_EXTERNAL_BASE_URL` | `http://localhost:8097` | backend(Go実装)の外部公開APIの接続先 |
| `UPSTREAM_CONF_PATH` | `../upstream.conf`(sidecarから見た相対パス) | 書き換え対象ファイル |
| `NGINX_PREFIX_DIR` | `..`(sidecarから見た相対パス) | `nginx -s reload`実行時の`-p`引数 |

## 動作確認

```sh
TOKEN=$(curl -s -X POST http://localhost:8082/realms/training/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=external-api-client -d client_secret=<realm-export.jsonの値> \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")

curl http://localhost:8081/external/v1/tasks?user_id=1 -H "Authorization: Bearer $TOKEN"
```

`backend.task-language`をDBで変更後、サイドカーのログで`upstream.confを書き換えました`・
`nginxをreloadしました`が出ることを確認し、`upstream.conf`の中身が実際に書き換わっていることも確認する
値が実装済み言語("go")以外でも、フォールバックの警告ログが出た上で
`:8097`へのルーティングが維持されることを確認する

## テスト

```sh
cd sidecar && go test ./... -v
```

12件、DB接続不要ですべてpass(`parseLanguage`のJSONパース、`resolveTarget`の
フォールバックロジック、`renderUpstreamConf`のレンダリング)
nginx設定自体の検証は `nginx -p "$(pwd)" -c nginx.conf -t` で行う(構文チェック)

## Go製リバースプロキシ vs nginx+サイドカー方式、実装してみての所感

`gateway/go/README.md`の「実装してみての所感」参照(同じ比較を反対側の視点から書いている)
このnginx版で特に印象的だったのは、**「flag評価」と「実際のプロキシ処理」が完全に別プロセス・
別言語(サイドカーはGo、プロキシ本体はnginx)に分かれる**ため、両者の同期(ファイルの書き換えタイミングとreloadのタイミング)に一瞬のズレが生じうる点
今回は「ファイルの中身が変わった時だけreloadする」という単純な比較で十分だったが、
実務では複数のサイドカーインスタンスが同時に reload を叩き合うような競合(reload自体は原子的だが、
書き込み中の設定ファイルを読みに行くタイミング等)への配慮が必要になりうる、という学びがあった
