# E2E テスト一式(JS 3種 + Go 3種)

CONTRACT.md(`../CONTRACT.md`セクション9・18)に基づき、同一のシナリオを6フレームワークで実装している
各ディレクトリのREADMEにセットアップ・実行コマンド・選定理由を記載

**JS/Node製**:
- `playwright/` — 自動待機・トレース標準搭載。最も手厚く実装(認証・CRUD・Feature Flag)
- `cypress/` — クロスオリジン遷移を`cy.origin`で明示的に扱う必要がある点をPlaywrightと比較できる
- `selenium/` — Node.js + selenium-webdriver + mocha。W3C WebDriverプロトコル経由の「本来のSelenium」

**Go製**(CONTRACT.mdセクション18、JS版との比較学習用):
- `chromedp/` — Chrome DevTools Protocolを直接叩く、最も歴史の長いGo製ブラウザ自動化ライブラリ
- `go-rod/` — 同じくCDPベースだが、よりチェーン可能なAPI設計。chromedpの対抗馬
- `playwright-go/` — Playwright本体(Node製ドライバ)をGoから操作するコミュニティ製バインディング

共通のselector規約は `../SELECTORS.md` を参照(frontend実装はここに定義した`data-testid`を付与すること)

## カバーしているシナリオ(全6フレームワーク共通)

1. 未ログイン状態で`/tasks`→ローカル認証(HMAC版・既定)のログイン画面へリダイレクト
2. ローカル認証(HMAC版)でログイン→Task一覧表示→ログアウト→ログイン画面に戻る
3. ローカル認証(RSA版)でログイン→Task一覧表示
4. 誤ったパスワードでのログイン→エラーメッセージ表示
5. Keycloakでログイン→Task一覧表示→ログアウト→ログイン画面に戻る
6. Task作成→一覧に反映→更新→一覧に反映→削除→一覧から消える

Playwright(JS)のみ追加で `frontend.tasks-ts-rewrite` Feature FlagのON/OFF切り替えテストを実装

さらに、JS製3種(playwright/cypress/selenium)には、Task登録UXの3パターン
(inline/modal/page、CONTRACT.mdセクション19・19.7、多値Feature Flag`frontend.task-create-ux`)
のうちmodal版・page版のシナリオ(`task-create-ux.*`)も追加済み(inline版は上記6の通り既存シナリオでカバー)

さらに、パスキー(WebAuthn、CONTRACT.mdセクション22)のログイン・登録シナリオ(`passkey.*`)を、
Cypressを除く5フレームワーク(playwright/selenium/chromedp/go-rod/playwright-go)に追加済み。
実機の生体認証を使わず、各フレームワークの仮想認証器機能(CDPの`WebAuthn`ドメイン、または
SeleniumのWebDriver Virtual Authenticator拡張)で自動化している。詳細は`../SELECTORS.md`の
「パスキー」節、Cypressを見送った理由は`cypress/README.md`参照。

## 既知の制約

- CIでのFeature Flag自動切り替え(admin画面での変更→bff再起動)は未実装。手動運用手順のみ(各READMEに記載)
- 各フレームワークとも、実装後に一度通しで実行してフレーキーな箇所が無いか確認すること

【2回目のe2e監査で追加】admin/go・admin/rails(Feature Flag管理・ユーザー管理・パスキー登録状況の
表示)は、これまで6フレームワークいずれのe2e対象にもなっていなかった。Playwrightに
`tests/admin.spec.ts`として最低限のシナリオを追加した(他5フレームワークへの移植は未着手、
詳細は`playwright/README.md`参照)。

同じくPlaywrightに`tests/resilience.spec.ts`として、ネットワーク遅延・ブラウザの戻る/リロード・
複数タブでのセッション共有・パスキー登録後のパスワード認証の後方互換、といった土台部分の挙動を
追加し、その後**Cypress以外の5フレームワーク(Selenium/chromedp/go-rod/playwright-go)にも
同じシナリオを移植した**(`selenium/README.md`・`chromedp/README.md`・`go-rod/README.md`・
`playwright-go/README.md`参照)。Cypressは複数タブ・パスキー登録前提のシナリオを
仕様上サポートできないため、2シナリオ(ネットワーク遅延・ブラウザバック/リロード)のみ
移植した(`cypress/README.md`参照)。移植の過程でgo-rodだけ複数タブまわりのCDPレベルの
ハングに何度も遭遇し、ログアウト伝播シナリオを`t.Skip`で見送った(`go-rod/README.md`参照、
Playwright/Selenium/chromedp/playwright-goでは同じシナリオが問題無く動いている)。

【3回目のe2e監査で追加】Playwrightに`tests/security.spec.ts`として、session_id Cookie改ざん→401・
CSRFトークン欠落→403・XSSペイロード実地確認・ログアウト後の情報露出確認、の4シナリオを追加し
(CONTRACT.mdセクション23参照)、**今回は6フレームワーク全てに移植できた**(Cypress含む。
WebAuthn仮想認証器・複数タブのような構造的制約に依存しないシナリオだったため)。
各フレームワークのREADMEに、Cookie操作・fetch実行・alertダイアログ検出まわりの
実装上の工夫を記載している。
