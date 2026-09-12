# go-rod版 E2E

CONTRACT.mdセクション18(E2EのGo実装追加)に基づく、[go-rod](https://github.com/go-rod/rod)を
使ったGo製のe2eテスト。`../playwright/`(JS版)と全く同じシナリオを実装している。
共通のselector規約は`../SELECTORS.md`を参照。

## セットアップ・実行

```sh
cd e2e/go-rod
go mod tidy   # 初回のみ(github.com/go-rod/rodを取得)
```

前提として、以下が起動していること(README.md「セットアップ手順」参照)。

```sh
docker compose up -d --wait mysql redis keycloak swagger-ui frontend
cd backend && go run ./cmd/migrate up && go run ./cmd/server &
cd bff && go run ./cmd/server &
```

```sh
go test ./...
# 詳細ログ付き
go test -v ./...
```

初回実行時、go-rodがヘッドレスChromiumを自動ダウンロードする(`~/.cache/rod/browser/`配下、
数十秒かかる)。2回目以降はキャッシュされたブラウザを使うため高速。

### 環境変数

`../SELECTORS.md`の一覧(`FRONTEND_BASE_URL`・`E2E_USERNAME`・`E2E_PASSWORD`・
`E2E_LOCAL_EMAIL`・`E2E_LOCAL_PASSWORD`)と共通。

## カバーしているシナリオ

`../playwright/tests/auth.spec.ts`・`task-crud.spec.ts`と同一(`../README.md`参照)。

- `auth_test.go`: 未ログインリダイレクト、ローカルHMAC/RSA両ログイン+ログアウト、
  パスワード誤り時のエラー表示、Keycloakログイン+ログアウト
- `task_crud_test.go`: 作成→一覧反映→更新→一覧反映→削除→一覧から消える(inline版)
- `task_create_ux_test.go`: CONTRACT.mdセクション19・19.7のTask登録UX3パターンのうち
  modal版・page版。多値Feature Flag`frontend.task-create-ux`をMySQLへ直接接続して一時的に
  切り替え、終了後は必ず既定値(`inline`)へ戻す(`feature_flag_helpers.go`)。
  接続先は環境変数`MYSQL_DSN`(既定`root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true`)

## 実装してみた所感(JS版・chromedpとの比較)

### Playwright(JS版)との違い

- **APIの薄さ**: Playwrightの`page.getByTestId(...)`のような専用APIは無く、CSS属性セレクタ
  (`[data-testid="..."]`)を自分で組み立てる必要がある(本実装では`testid()`ヘルパーで吸収した)
- **自動待機の粒度**: Playwrightの`fill()`/`click()`は要素の可視性・操作可能性を暗黙に待つが、
  go-rodは`MustElement`(DOM存在まで待つ)と`MustWaitVisible`(可視まで待つ)が別メソッドのため、
  「存在はするが描画されるまで一瞬ラグがある」要素では明示的に`.MustWaitVisible()`を挟む必要がある場面があった
- **Reactの制御コンポーネント対応**: `<select>`と`<input type="date">`は、go-rodの`MustInput`/`MustSelect`
  経由だとReactのonChangeが正しく発火しないケースがあり、`e2e/selenium`(JS版)が採用したのと同じ
  「ネイティブのvalueセッターを直接呼んでinput/changeイベントを発火させる」手法(`setValueViaJS`)に
  頼る必要があった。Playwright(JS版)の`fill()`/`selectOption()`はこの問題が起きず、この差は
  「PlaywrightがCDPの薄いラッパーではなく、フォーム操作をブラウザエンジンの入力イベントとして
  忠実にシミュレートする層を独自に持っている」ことの表れだと感じた
- **ダイアログ処理**: `page.MustHandleDialog()`が`(wait func(), handle func(bool, string))`という
  「ダイアログを開くトリガーをgoroutineで実行しつつ、waitでブロックしてhandleで応答する」という
  非同期パターンになっており、Playwrightの`page.once("dialog", ...)`より一手間多い。ただしこの設計は
  「ダイアログが本当に開くまで待つ」ことを型で強制する分、タイミングの取り違えによる
  フレーキーさは逆に起きにくいとも言える

### chromedp(CONTRACT.mdセクション18のもう一つのGo版選択肢)との比較

chromedpは「一連の操作を`chromedp.Run(ctx, chromedp.Navigate(...), chromedp.Click(...), ...)`のように
アクションのスライスとして宣言する」設計であるのに対し、go-rodは`page.MustElement(...).MustClick()`と
Go的なメソッドチェーンで書ける。今回go-rodで実装した体感としては、後者の方が「今何の要素に対して
何をしているか」がコード上で直感的に追いやすく、一般に言われる「go-rodの方がchromedpより書きやすい」
という評判は妥当だと感じた。一方chromedpは`context.Context`ベースでタイムアウト・キャンセルを
標準の`context`パッケージの流儀に統一できる点は、Goの他のコードとの一貫性という意味で分がある。

## 既知の制約

- ヘッドレスモード固定(`rod.New().MustConnect()`の既定)。ブラウザを表示して確認したい場合は
  `rod.New().ControlURL(launcher.New().Headless(false).MustLaunch()).MustConnect()`のように変更する
- **複数タブでのログアウト伝播シナリオは未実装(意図的にスキップ)**: 「片方のタブでログアウトすると、
  もう片方のタブも次のアクセスで未ログイン扱いになる」の確認(`resilience_test.go`の
  `TestResilience_MultiTab_LogoutPropagation`)は、go-rod v0.116.2 + この環境の組み合わせで
  未解決のCDPハングに繰り返し当たったため、`t.Skip`で見送っている。tab1でログアウト操作を
  終えた直後にtab2へ何らかのコマンドを送ると、CDPの応答が返らずOSソケットの読み取りで
  ブロックしたまま戻ってこない現象で、コマンドの種類を変える・独立した2本のCDP接続に
  分離する・Goのcontext経由のTimeoutを設定する、等一通り試したが解決しなかった
  (詳細な試行錯誤の記録は同関数のコメント参照)。「片方のタブでログインすると、もう片方の
  タブでも認証済み扱いになる」という前半のシナリオ(`TestResilience_MultiTab_SessionShared`)は
  問題無く動く。Playwright/Selenium/chromedp版では同種のログアウト伝播シナリオが問題無く
  動いているため、go-rod固有(またはこの環境固有)の制約と判断している。

## ネットワーク遅延・ブラウザ操作・パスキー登録の後方互換(resilience_test.go)

2回目のe2e監査で追加。「壊れやすいのに見落とされがちな、アプリの土台部分の挙動」を対象にする:
`/api/tasks`応答が遅い間のローディング表示、ブラウザバック/リロードでの認証維持、
複数タブでのセッション共有(上記の既知の制約参照)、パスキー登録後もパスワードログインが
壊れていないことの回帰確認。ネットワーク遅延は`page.HijackRequests()`(go-rodがCDPの
Fetchドメインをラップした高レベルAPI)で実現している。

## パスキー(WebAuthn、CONTRACT.mdセクション18・22)

go-rodも`lib/proto`パッケージにCDPの`WebAuthn`ドメインの型定義一式(`proto.WebAuthnEnable`・
`proto.WebAuthnAddVirtualAuthenticator`等)を持っており、chromedp版とほぼ同じ量のコードで
仮想認証器を登録できる。`proto.WebAuthnXxx{...}.Call(page)`という統一された呼び出し方は、
go-rodの他のラップされていないCDPコマンドとも共通の作法。

**実機検証で発覚した問題**: go-rodの既定動作(`rod.New().MustConnect()`)は、go-rodが
自動ダウンロードする特定リビジョンのChromium(実機確認時点で`Chromium 128.0.6568.0`)に
接続するが、このビルドは`PublicKeyCredential.parseCreationOptionsFromJSON`/
`parseRequestOptionsFromJSON`(frontendが機能検出に使うWebAuthn Level 3のJSON直列化API)を
持っておらず、「このブラウザはパスキーに対応していません」という`PasskeyUnsupportedError`が
発生してテストが恒久的にタイムアウトした(仮想認証器の登録自体はCDPレベルでは成功していたため、
原因の特定に`document.body.innerHTML`を直接評価する簡易デバッグが必要だった)。
これはgo-rod自体のバグではなく、go-rodが同梱するオープンソースのChromiumスナップショットが、
Google Chrome(ブランド版)より一部の新しいWeb Platform APIの有効化が遅れることがある、
という配布形態の違いに起因する。対応として、`passkey_test.go`だけシステムの実Google Chrome
(環境変数`CHROME_BIN`、既定はmacOSの標準インストール先)を明示的に指定して起動するようにした
(他の既存シナリオは引き続きgo-rod既定のChromiumで動く)。

```bash
go test ./... -run TestPasskeyRegisterAndLogin -v
```

## セキュリティ(security_test.go、CONTRACT.mdセクション23)

3回目のe2e監査(セキュリティ観点)で追加。`../playwright/tests/security.spec.ts`の4シナリオを移植。

**実機検証で判明した罠**: `page.MustEval(js)`に渡す文字列は「関数リテラル」として扱われ、
go-rodが内部でこれに引数を`apply`する。Playwright/chromedp版のような即時実行IIFE
(`(async()=>{})()`)をそのまま渡すと、戻り値(Promise、関数ではない)に対して`apply`しようと
して`TypeError: (intermediate value)(...).apply is not a function`という分かりにくいエラーに
なる。末尾の`()`を付けない`async () => {...}`という関数式のまま渡す必要がある。

alertの検出は`page.EachEvent(func(e *proto.PageJavascriptDialogOpening) {...})`
(持続的なイベントリスナー、goroutineで実行)を使い、chromedp版の`ListenTarget`と同じ考え方で
type=alertのものだけをカウントする(既存の`MustHandleDialog`は「次の1回だけ」を待つ
ワンショットAPIのため今回の用途には使えない)。

```bash
go test ./... -run TestSecurity -v
```

## 多言語backend切り替えシナリオ(2026-09追記)

- `backend_task_language_test.go`: `backend.task-language`をrustへ切り替え、Task CRUDが引き続き動くことを確認。`backend-rust`が起動していなければskip
- `backend_external_tasks_orm_test.go`: `backend.external-tasks-orm`をbobへ切り替え、外部公開APIの応答形状を確認
- `passkey_frontend_rails_without_bff_test.go`: `frontend-rails/without-bff`(CONTRACT.mdセクション22.9)独自のパスキー登録・ログインを確認
