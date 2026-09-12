# E2E (Selenium)

## セットアップ

```bash
cd training-go/bff-gin/e2e/selenium
npm install
```

Chromeがインストール済みであること

【実機検証で判明】以前は`chromedriver`npmパッケージ(特定バージョン固定)に依存していたが、
インストール済みChromeのバージョンと固定されたchromedriverのバージョンが合わないと`SessionNotCreatedError`で起動できなかった
selenium-webdriver 4.6以降に内蔵されている
Selenium Managerが、インストール済みChromeのバージョンに合わせて自動でdriverを解決・ダウンロードしてくれるため、`chromedriver`パッケージへの依存は削除した

## 実行

```bash
FRONTEND_BASE_URL=http://localhost:5173 \
E2E_USERNAME=general-user \
E2E_PASSWORD=password \
npm test
```

ヘッドレスを無効化してブラウザ操作を目視したい場合は `HEADLESS=false npm test`

## 選定理由(Playwright / Cypress との比較)

- **Playwright**: 自動待機・トレース機能が標準搭載
  - 3種の中で最も安定して書きやすい
- **Cypress**: ブラウザ内で実行されるアーキテクチャのため、クロスオリジン遷移(Keycloakログイン)を`cy.origin`で明示的に扱う必要がある
- **Selenium (selenium-webdriver)**:
  - W3C WebDriverプロトコル経由でchromedriverを介してブラウザを操作する、最も歴史が長く枯れた方式
  - `until.elementLocated`による明示的な待機を自分で書く必要があり、暗黙的な自動待機を持つPlaywrightと比べて記述量が増える
  - `training-go/gin/test/e2e`のchromedp実装はCDP(Chrome DevTools Protocol)を直接叩く方式でSeleniumとは別物(WebDriverを経由しない)
  - 今回は「本来のSelenium」を体験する目的
    - Node.js + selenium-webdriver + mocha の構成をあえて選んだ
    - (Go実装に寄せて chromedp で代替する案もあったが、それは技術的にはSeleniumではなくchromedpの再掲になってしまうため採用しなかった)

## Task登録UX(3パターン、CONTRACT.mdセクション19・19.7)

`frontend.task-create-ux`をMySQL側(admin/go・admin/rails経由)で`modal`/`page`に切り替えてから
実行する(既定の`inline`は`task-crud.test.js`でカバー済み)

```bash
npx mocha test/task-create-ux.test.js --timeout 30000
```

## パスキー(WebAuthn、CONTRACT.mdセクション22)

CDPを直接叩く他フレームワークとは異なり、Selenium 4のWebDriver仕様が標準化した
Virtual Authenticator拡張(`driver.addVirtualAuthenticator(options)`)を使う

ブラウザ非依存の標準APIとして定義されている点がPlaywright/chromedp/go-rod/playwright-go(いずれもCDP直叩き、
事実上Chromium限定)との対比になる

```bash
npx mocha test/passkey.test.js --timeout 30000
```

## ネットワーク遅延・ブラウザ操作・複数タブ・パスキー登録の後方互換(resilience.test.js)

2回目のe2e監査で追加
「壊れやすいのに見落とされがちな、アプリの土台部分の挙動」を対象にする:
`/api/tasks`応答が遅い間のローディング表示、ブラウザバック/リロードでの認証維持、複数タブでの
セッション共有・ログアウト伝播(`driver.switchTo().newWindow("tab")`、Selenium 4の機能)、
パスキー登録後もパスワードログインが壊れていないことの回帰確認

**実装時の発見**: ネットワーク遅延の実現には`driver.createCDPConnection()`でCDPのFetchドメインを
使うが、このバージョンのselenium-webdriverが公開している`CDPConnection`クラスには、特定コマンドの
応答を待つ`send()`と応答を待たない`execute()`しか無く、`Fetch.requestPaused`のような非同期
イベントを継続的に受け取るための`.on()`のような公開APIが存在しない(`node_modules/selenium-webdriver/devtools/CDPConnection.js`参照)

内部で保持している`_wsConnection`(生のWebSocket、非公開プロパティ)へ直接`.on('message', ...)`することでイベント購読を実現している
chromedp/go-rod が同じ CDP の Fetchドメインを型付きの公開APIとして扱えるのとは対照的に、Selenium ではこの部分だけ「ライブラリの非公開実装詳細に踏み込む」必要があった

```bash
npx mocha test/resilience.test.js --timeout 40000
```

## セキュリティ(security.test.js、CONTRACT.mdセクション23)

3回目のe2e監査(セキュリティ観点)で追加
`../playwright/tests/security.spec.ts`の4シナリオを移植

session_id CookieはHttpOnlyのため`document.cookie`からは触れないが、
`driver.manage().getCookie`/`addCookie`はWebDriver標準のブラウザ特権APIとして扱えるため問題なく改ざんできる

fetch の実行は `driver.executeAsyncScript`(コールバック引数を呼ぶまで待つ版)を使う必要があった

通常の`executeScript`はPromiseの解決を待たず、返り値がPromiseオブジェクトそのものになってしまう(classic WebDriverの既知の制約)
alertの検出は`window.alert`自体を上書きしてフラグを立てる方式にした(selenium-webdriverには統一ダイアログイベントAPIが無いため)

```bash
npx mocha test/security.test.js --timeout 30000
```

## 多言語backend切り替えシナリオ(2026-09追記)

- `backend-task-language.test.js`: `backend.task-language`をrustへ切り替え、Task CRUDが引き続き動くことを確認(要`backend-rust`起動)
- `backend-external-tasks-orm.test.js`: `backend.external-tasks-orm`をbobへ切り替え、外部公開APIの応答形状をNode組み込みの`fetch`で確認(ブラウザ操作は行わない)
- `passkey_frontend_rails_without_bff.test.js`: `frontend-rails/without-bff`(CONTRACT.mdセクション22.9)独自のパスキー登録・ログインを確認(要`frontend-rails/without-bff`起動)
