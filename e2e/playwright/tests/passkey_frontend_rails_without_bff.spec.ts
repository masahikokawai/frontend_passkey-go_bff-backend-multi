import { test, expect } from "@playwright/test";

// CONTRACT.mdセクション22.9(2026-09-11追記、frontend-rails/without-bffへのパスキー追加)
//
// 【なぜこのテストが必要か】ランブックに「e2e未実装(コア構成のReact+bffフローのみが対象)」と
// 明記されていた既知のギャップだった。既存のpasskey.spec.ts(bffのローカル認証ユーザー向け、
// セクション22.1)とは全く別の対象(Keycloak発行ユーザー向けパスキー)を検証する。
//
// 【baseURLについて】このアプリ(:5174)はplaywright.config.tsのbaseURL(React frontend、:5173)とは
// 別オリジンのRailsアプリのため、admin.spec.tsと同じパターン(絶対URLでpage.goto)で扱う。
//
// 【data-testidを使わない理由】このアプリはe2e/SELECTORS.mdのdata-testid規約に従っておらず、
// 素のRails ERBビューに素朴なid属性のみを付与している(login.html.erb/account.html.erb参照)。
const FRONTEND_RAILS_WITHOUT_BFF_BASE_URL =
  process.env.FRONTEND_RAILS_WITHOUT_BFF_BASE_URL ?? "http://localhost:5174";

test.describe("frontend-rails/without-bff パスキー(WebAuthn)", () => {
  test("Keycloakでログイン→パスキー登録→ログアウト→パスキーのみでログイン", async ({ page }) => {
    const client = await page.context().newCDPSession(page);
    await client.send("WebAuthn.enable");
    await client.send("WebAuthn.addVirtualAuthenticator", {
      options: {
        protocol: "ctap2",
        transport: "internal",
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    });

    await page.goto(FRONTEND_RAILS_WITHOUT_BFF_BASE_URL + "/login");

    // 「Keycloakでログイン」はRailsのbutton_to(POSTフォーム送信)で、id/data-testidを持たないため、
    // 表示テキストで要素を特定する
    await page.getByRole("button", { name: "Keycloakでログイン" }).click();
    await page.locator("#username").fill(process.env.E2E_USERNAME ?? "general-user");
    await page.locator("#password").fill(process.env.E2E_PASSWORD ?? "password");
    await page.locator("#kc-login").click();
    await page.waitForURL(FRONTEND_RAILS_WITHOUT_BFF_BASE_URL + "/welcome", { timeout: 15_000 });

    // パスキー登録: /account画面の「パスキーを登録」ボタン
    await page.goto(FRONTEND_RAILS_WITHOUT_BFF_BASE_URL + "/account");
    await page.locator("#passkey-register-button").click();
    await expect(page.locator("#passkey-status")).toContainText("登録しました", { timeout: 15_000 });

    // ログアウト(welcome.html.erbのボタンはbutton_to(DELETE)だが、config/routes.rbが
    // 「ブラウザから直接叩いての手動確認をしやすくするため」GET /logoutも許可しているため、
    // E2Eでもそちらを使う)
    await page.goto(FRONTEND_RAILS_WITHOUT_BFF_BASE_URL + "/logout");

    // ここから先はKeycloakを一切使わない、パスキーのみでのログイン
    await page.goto(FRONTEND_RAILS_WITHOUT_BFF_BASE_URL + "/login");
    await page.locator("#passkey-login-button").click();
    await page.waitForURL(FRONTEND_RAILS_WITHOUT_BFF_BASE_URL + "/welcome", { timeout: 15_000 });

    // welcome.html.erbは`ログイン方式: <%= session[:auth_mode] %>`をそのまま出力する
    await expect(page.locator("body")).toContainText("ログイン方式: passkey");
  });
});
