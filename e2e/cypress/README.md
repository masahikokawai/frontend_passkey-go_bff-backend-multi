# E2E (Cypress)

## セットアップ

```bash
cd training-go/bff-gin/e2e/cypress
npm install
```

## 実行

```bash
FRONTEND_BASE_URL=http://localhost:5173 \
CYPRESS_E2E_USERNAME=general-user \
CYPRESS_E2E_PASSWORD=password \
CYPRESS_KEYCLOAK_BASE_URL=http://localhost:8082 \
npm test
```

## 選定理由・注意点

CypressはPlaywrightと異なり、既定でクロスオリジン遷移([`cy.origin`](https://docs.cypress.io/api/commands/origin))を明示的に扱う必要がある
BFFパターンではログイン時にKeycloak(別オリジン)へ実際に302リダイレクトするため、`cy.origin`でKeycloak側の操作を明示的に分離している

Playwright実装(`../playwright/`)はこの制約がなく素直に書けるため、同じ導線でも記述量・可読性に差が出る点を比較学習できる

## Task登録UX(3パターン、CONTRACT.mdセクション19・19.7)

`frontend.task-create-ux`をMySQL側(admin/go・admin/rails経由)で`modal`/`page`に切り替えてから
実行する(既定の`inline`は`task-crud.cy.js`でカバー済み)

```bash
npx cypress run --spec "cypress/e2e/task-create-ux.cy.js"
```

## パスキー(WebAuthn)のE2Eは今回見送り(CONTRACT.mdセクション22)

他の5フレームワーク(Playwright(JS)・Selenium・chromedp・go-rod・playwright-go)には
CDPのWebAuthnドメイン(またはSeleniumのWebDriver Virtual Authenticator拡張)を使った
仮想認証器ベースのパスキーE2Eを追加したが、**Cypress(現行バージョン13.15系)には
これに相当する一級市民のAPIが無い**ため、今回は見送った。

- Cypressは内部的にはCDPを使っているが、spec側のコード(`cy.*`コマンド)から任意のCDPコマンド
  (`WebAuthn.enable`・`WebAuthn.addVirtualAuthenticator`)を直接呼べる公式APIが提供されていない
  (`Cypress.automation("remote:debugger:protocol", ...)`はプラグイン/イベントハンドラ層でのみ
  想定された内部APIで、spec側から素朴に使う経路ではない)
- サードパーティのプラグイン(`cypress-webauthn`等)を使えば実現できる可能性はあるが、
  今回のプロジェクトが依存する範囲を増やしてまで導入するほどの優先度ではないと判断した
- Cypress本体でのWebAuthn/Virtual Authenticatorサポートは、Cypress側のissueとしても
  長期間未解決の要望として残っている状態

もしCypressでのパスキーE2Eが必要になった場合は、上記プラグインの採用検討、または
Cypressのバージョンアップでの標準サポート状況を再確認すること。

## ネットワーク遅延・ブラウザ操作の後方互換(resilience.cy.js、一部見送り)

2回目のe2e監査で追加。「壊れやすいのに見落とされがちな、アプリの土台部分の挙動」を対象にする。
他5フレームワークでは4シナリオ(ネットワーク遅延・ブラウザバック/リロード・複数タブでの
セッション共有/ログアウト伝播・パスキー登録後のパスワードログイン回帰確認)を実装したが、
**Cypressでは2シナリオ(ネットワーク遅延・ブラウザバック/リロード)のみ移植した**。

- **複数タブ**: Cypressは仕様上、複数タブ/ウィンドウの同時制御を公式サポートしていない
  (上記のWebAuthn仮想認証器と同じ既知の制約)
- **パスキー登録後のパスワードログイン回帰確認**: このシナリオは「実際にパスキーを登録する」
  ステップを前提にしているが、Cypressには`passkey.cy.js`相当のテスト自体が存在しない
  (上記の通りWebAuthn仮想認証器に対応していないため)。同じ制約により、この回帰確認シナリオも
  Cypressには移植できない

ネットワーク遅延の実現は`cy.intercept()`にレスポンスの`setDelay()`を組み合わせるだけで済み、
今回移植した5フレームワークの中で最も簡潔に書けた(Playwrightの`page.route()`と並ぶ
使いやすさで、CDPのFetchドメインを直接扱う他のGo製フレームワーク・Seleniumより手数が少ない)。

```bash
npx cypress run --spec "cypress/e2e/resilience.cy.js"
```

## セキュリティ(security.cy.js、CONTRACT.mdセクション23)

3回目のe2e監査(セキュリティ観点)で追加。`../playwright/tests/security.spec.ts`の4シナリオ
(session_id Cookie改ざん→401・CSRFトークン欠落→403・XSSペイロード実地確認・ログアウト後の
情報露出確認)全てを、Cypress標準API(`cy.setCookie`/`cy.getCookie`・`window:alert`/
`window:confirm`イベント・`cy.window().then(win => win.fetch(...))`)だけで移植できた。
WebAuthn仮想認証器・複数タブのような、Cypress側に構造的な制約がある機能を一切使わないため、
今回のセキュリティ4シナリオに限っては見送りが不要だった(パスキー・複数タブとは対照的)。

```bash
npx cypress run --spec "cypress/e2e/security.cy.js"
```

## 多言語backend切り替えシナリオ(2026-09追記)

- `backend-task-language.cy.js`: `backend.task-language`をrustへ切り替え、Task CRUDが引き続き動くことを確認(要`backend-rust`起動。フラグ切り替えは手動での事前操作が前提、`task-create-ux.cy.js`と同じ既存の運用に合わせている)
- `backend-external-tasks-orm.cy.js`: `backend.external-tasks-orm`をbobへ切り替え、外部公開APIの応答形状を`cy.request`で確認(ブラウザ操作は行わない)

frontend-rails/without-bffのパスキーシナリオはCypress未実装(既存のWebAuthn仮想認証器非対応という制約による、セクション18.5参照)
