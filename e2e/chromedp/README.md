# e2e (Go + chromedp)

CONTRACT.mdセクション18: `e2e/playwright`(JS)と全く同じシナリオを、Go製の
[chromedp](https://github.com/chromedp/chromedp)で実装したもの。Chrome DevTools Protocol(CDP)を
直接叩くライブラリで、外部プロセス(Selenium Server等)は不要。

## セットアップ

```sh
cd e2e/chromedp
go mod download
```

Chromeがインストール済みであること(既定のパスから自動検出される。特別なドライバのダウンロードは不要)。

## 実行

```sh
# training-go/bff-gin ルートで
docker compose up -d --wait mysql redis keycloak swagger-ui
cd backend && go run ./cmd/migrate up && go run ./cmd/server &
cd bff && go run ./cmd/server &
cd frontend && npm run dev &

cd e2e/chromedp
go test ./... -v
```

環境変数は`../SELECTORS.md`参照(`FRONTEND_BASE_URL`・`E2E_USERNAME`・`E2E_PASSWORD`・
`E2E_LOCAL_EMAIL`・`E2E_LOCAL_PASSWORD`、いずれも既定値のままで動く)。

## カバーしているシナリオ

`../SELECTORS.md`・`../playwright/tests/auth.spec.ts`・`task-crud.spec.ts`と同一(6シナリオ)。
`auth_test.go`・`task_crud_test.go`参照。

さらに、CONTRACT.mdセクション19・19.7のTask登録UX3パターン(inline/modal/page)のうち
modal版・page版を`task_create_ux_test.go`で検証する(inline版は上記`task_crud_test.go`でカバー済み)。
多値Feature Flag`frontend.task-create-ux`をMySQLへ直接接続して一時的に切り替え、テスト終了後は
必ず既定値(`inline`)へ戻す(`feature_flag_helpers.go`の`setTaskCreateUX`)。
接続先は環境変数`MYSQL_DSN`(既定`root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true`、
docker-compose.yamlのmysqlサービスに対応)。切り替え後はbff/backendのポーリング間隔
(既定10秒)分の反映待ちが必要なため、このシナリオだけ実行時間が長め(1件あたり15秒前後)になる。

## Playwright(JS版)と実装してみての所感

- **待機戦略が根本的に違う**: Playwrightは`getByTestId(...)`だけで「要素が現れるまで自動的に待つ」が、
  chromedpは`chromedp.WaitVisible(...)`を明示的にActionとして並べる必要がある。うっかり
  `WaitVisible`を書き忘れると、まだDOMに無い要素への`Click`が失敗する、という事故が起きやすい。
  Playwrightの「暗黙の自動待機」がいかに便利かを、無くしてみて初めて実感できた。
- **Reactの制御されたinputへの値設定が鬼門**: `<input type="date">`・`<select>`へ値を入れる際、
  単純に`.value`を代入するだけではReactのonChangeが発火しない(このプロジェクトのSelenium実装で
  既に踏んでいた既知の問題と全く同じ)。`helpers.go`の`setReactValue`で、prototype側の本来の
  setterを直接呼んでReact独自の値追跡を回避し、`input`/`change`イベントを手動でdispatchする
  対処が必要だった。Playwrightの`selectOption`/`fill`はこの種の問題を内部で吸収してくれている。
- **ダイアログハンドリング**: Playwrightは`page.once("dialog", ...)`と直感的だが、chromedpは
  `chromedp.ListenTarget`でCDPの`Page.javascriptDialogOpening`イベントを直接リッスンし、
  `page.HandleJavaScriptDialog(true)`で応答する、という一段低レイヤーのAPIになる。書く量は
  増えるが、CDPが実際に何をやっているかがそのまま見える分、学習用途としては面白い。
- **`chromedp.Clear`はReactの制御されたテキストinputを実際にはクリアしない**(実機検証で発覚):
  既存値が入ったテキストinputの値を書き換える際、`chromedp.SetValue`は"could not set value on node"で
  失敗し、代替の`chromedp.Clear`は**エラーにならず成功したように見えるが、実際にはDOM上の値が
  変わっていない**という、より厄介な挙動だった(直後に`chromedp.Value`で読み直して初めて気づいた)。
  結果、続けて`SendKeys`すると既存値の末尾に新しい値が連結され(例: `"foo"` + `"foo-upd"` →
  `"foofoo-upd"`)、テストは「タイムアウトで失敗」という形でしか異常に気づけなかった。最終的に
  日付/select用に既に用意していた`setReactValue`(prototype側setterの直接呼び出し)をテキスト
  inputにもそのまま使うことで解決した。Playwrightの`fill()`は内部で選択→削除→入力を適切に
  行うため、この種の罠を踏まずに済む。
- **外部プロセス不要な点は素直に楽**: SeleniumのようにWebDriverサーバーを別途立てる必要が無く、
  `go test`一発でブラウザプロセスが自動起動する。CI環境へのセットアップが最も軽量な選択肢になりそう。
- **エラーメッセージの読みにくさ**: chromedpのエラーは「どのActionのどこで失敗したか」が
  Playwrightほど親切に出ない(タイムアウト時に「何を待っていたか」の文脈が薄い)。デバッグ時は
  `chromedp.WithDebugf`を有効にしてCDPの生ログを追う必要があり、Playwrightの
  トレースビューア(`trace: "on-first-retry"`)のような可視化ツールが標準では無い点は不便だった。

## 動作確認について

初回実装時はDocker daemonが利用できない環境だったため通しテストが未実施だったが、その後
Docker Compose・backend/bff/frontendが起動している環境で全シナリオ(認証5件・Task CRUD・
Task登録UX modal/page版2件、計8件)を実行し、全てpassすることを確認済み(`auth_test.go`・
`task_crud_test.go`・`task_create_ux_test.go`)。

## パスキー(WebAuthn、CONTRACT.mdセクション18・22)

`github.com/chromedp/cdproto/webauthn`パッケージでCDPの`WebAuthn`ドメインを型付きで直接叩ける。
`webauthn.Enable()`→`webauthn.AddVirtualAuthenticator(...)`の2アクションを足すだけで済み、
6フレームワーク中もっとも簡潔に書けた。

```bash
go test ./... -run TestPasskeyRegisterAndLogin -v
```

## セキュリティ(security_test.go、CONTRACT.mdセクション23)

3回目のe2e監査(セキュリティ観点)で追加。`../playwright/tests/security.spec.ts`の4シナリオ
(session_id Cookie改ざん→401・CSRFトークン欠落→403・XSSペイロード実地確認・ログアウト後の
情報露出確認)をそのまま移植した。

session_id Cookieの読み書きには、CDPの`Network.getCookies`/`Network.setCookie`が必要になる
(HttpOnly Cookieのため`document.cookie`からは触れない、意図的な設計)。XSSシナリオの
「alertが実際に発火したか」の判定は、`page.MustHandleDialog()`(既存task_crud_test.goで使う
「次の1回だけ待つ」ワンショットAPI)ではなく、`chromedp.ListenTarget`による持続的なリスナーで
`page.EventJavascriptDialogOpening.Type`が`alert`のものだけをカウントする方式にした
(削除確認ダイアログのconfirmと混同しないため)。

```bash
go test ./... -run TestSecurity -v
```

## 多言語backend切り替えシナリオ(2026-09追記)

- `backend_task_language_test.go`: `backend.task-language`をrustへ切り替え、Task CRUDが引き続き動くことを確認。`backend-rust`が起動していなければ`t.Skip`(新設したポート疎通確認による見送り規約)
- `backend_external_tasks_orm_test.go`: `backend.external-tasks-orm`をbobへ切り替え、外部公開APIの応答形状を確認
- `passkey_frontend_rails_without_bff_test.go`: `frontend-rails/without-bff`(CONTRACT.mdセクション22.9)独自のパスキー登録・ログインを確認
