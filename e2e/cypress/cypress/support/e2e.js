// frontendの /login は「ローカル認証(HMAC版・既定)のログインフォーム」であり、Keycloakへは自動遷移しない(CONTRACT.md セクション16)
// 未ログイン状態でcy.visit()した
// 保護ページは、useAuthが叩く/api/meの401をapiFetchが検知し /login?redirect=... へ遷移する
// Keycloakでログインしたい場合は、フォーム内の「Keycloakでログイン」ボタンを明示的にクリックしてから cy.origin でKeycloak側へ越境してフォーム操作する
Cypress.Commands.add("loginViaKeycloak", () => {
  const username = Cypress.env("E2E_USERNAME") || "general-user";
  const password = Cypress.env("E2E_PASSWORD") || "password";

  cy.getByTestId("login-keycloak-button").click();

  cy.origin(
    Cypress.env("KEYCLOAK_BASE_URL") || "http://localhost:8082",
    { args: { username, password } },
    ({ username, password }) => {
      cy.get("#username").type(username);
      cy.get("#password").type(password);
      cy.get("#kc-login").click();
    }
  );
  cy.getByTestId("task-list", { timeout: 15000 }).should("be.visible");
});

// ローカル認証(HMAC版・RSA版共通)のログインフォームからのログイン
// (CONTRACT.md セクション16.1: /login はHMAC版、/login/rsa はRSA版)
Cypress.Commands.add("loginViaLocal", (rsa = false) => {
  const email = Cypress.env("E2E_LOCAL_EMAIL") || "local-user@example.com";
  const password = Cypress.env("E2E_LOCAL_PASSWORD") || "password";

  if (rsa) {
    cy.getByTestId("login-rsa-link").click();
  }

  cy.getByTestId("login-email-input").type(email);
  cy.getByTestId("login-password-input").type(password);
  cy.getByTestId("login-submit-button").click();
  cy.getByTestId("task-list", { timeout: 15000 }).should("be.visible");
});

Cypress.Commands.add("getByTestId", (testId, options) => {
  return cy.get(`[data-testid="${testId}"]`, options);
});

// 【e2eのfeature flagクリーンアップ信頼性監査で追加】
// backend-task-language.cy.js・backend-external-tasks-orm.cy.jsはこれまで「事前に手動で
// MySQL/admin画面でflagを切り替えてから実行し、確認後は手動で戻す」という完全手動の運用だった。
// テストがassertion失敗で終わった場合、手動での戻し忘れがそのまま残り、後続の無関係な
// テスト実行が誤ったbackend言語/ORM実装に対して行われてしまうリスクがあった
// (Go版e2e(chromedp/go-rod/playwright-go)はt.Cleanupで自動復元しており、この差があった)。
//
// admin/goにはflag_keyで直接更新するAPIが無い(POST /flags/:idは数値の内部id必須)ため、
// 新しい依存(cypress-mysql等)を追加せずに自動復元するには、まずGET /(HTML)を
// Basic Auth付きで取得し、対象のflag_keyの行からidを正規表現で抜き出してからPOSTする、
// という2段階が必要になる。admin/goのHTML(admin/go/web/templates/index.html)は
// `<code>{flag_key}</code>` の後に同じ<tr>内で `href="/flags/{id}/edit"` が続く構造が
// 安定しているため、この方式で新規依存無しに自動復元できる
//
// 【実装時に発見した実際の罠】admin/goのUpdateハンドラは`enabled`フォーム項目が無いと
// `false`(HTMLのchecked無しcheckbox仕様)として扱うため、明示的に"on"を送らないと、
// default_variationを戻すつもりが誤ってflag自体を無効化してしまう
Cypress.Commands.add("restoreFeatureFlagViaAdmin", (flagKey, defaultValue) => {
  const adminBaseUrl = Cypress.env("ADMIN_GO_BASE_URL") || "http://localhost:8091";
  const adminUser = Cypress.env("ADMIN_BASIC_AUTH_USER") || "admin";
  const adminPassword = Cypress.env("ADMIN_BASIC_AUTH_PASSWORD") || "password";

  cy.request({
    method: "GET",
    url: adminBaseUrl + "/",
    auth: { username: adminUser, password: adminPassword },
    failOnStatusCode: false,
  }).then((indexRes) => {
    if (indexRes.status !== 200) return; // admin/go未起動等でも、この復元処理自体でテストを失敗させない

    const escapedKey = flagKey.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const match = new RegExp(`<code>${escapedKey}</code>[\\s\\S]*?href="/flags/(\\d+)/edit"`).exec(indexRes.body);
    if (!match) return;

    cy.request({
      method: "POST",
      url: `${adminBaseUrl}/flags/${match[1]}`,
      auth: { username: adminUser, password: adminPassword },
      form: true,
      body: { enabled: "on", default_variation: defaultValue },
      failOnStatusCode: false,
    });
  });
});
