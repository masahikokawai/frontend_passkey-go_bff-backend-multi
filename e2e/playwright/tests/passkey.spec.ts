import { test, expect } from "@playwright/test";
import { loginViaLocal } from "./helpers";

// CONTRACT.mdセクション22(パスキー/WebAuthn)
// 対象はbffのローカル認証(HMAC/RSA)ユーザーのみ
// (Keycloak発行ユーザーは対象外、22.1参照)
//
// 【なぜCDPSession経由なのか】Playwright(JS版)にはWebAuthn専用の高レベルAPIが無い
// (`page.setGeolocation`のような一級市民の機能としては提供されていない)
//
// しかし Chromium 限定で `page.context().newCDPSession(page)` から生の Chrome DevTools Protocol を直接叩けるため、
// `WebAuthn.enable` + `WebAuthn.addVirtualAuthenticator` で仮想認証器を登録すれば、
// 実機の生体認証を介さずに `navigator.credentials.create()`/`.get()`の一連のフローを実際に検証できる
//
// Go製3フレームワーク(e2e/chromedp・e2e/go-rod・e2e/playwright-go)にも全く同じ設定(protocol/transport/hasResidentKey等)で
// 揃えてあり、CDPというプロトコルは共通でも各ライブラリがどれだけ高レベルAPIを被せているかの比較対象にもなっている
test.describe("パスキー(WebAuthn)", () => {
  test("ローカル認証(HMAC)でログイン→パスキー登録→ログアウト→パスキーのみでログイン", async ({
    page,
  }) => {
    const client = await page.context().newCDPSession(page);
    await client.send("WebAuthn.enable");
    await client.send("WebAuthn.addVirtualAuthenticator", {
      options: {
        protocol: "ctap2",
        transport: "internal",
        // hasResidentKey: discoverable credential(CONTRACT.mdセクション22.2)の検証に必須
        // これが無いとallowCredentials指定無しのログイン時に認証器がcredentialを提示できない
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        // CIで人手のタッチ操作を待たせず即座に「本人確認OK」として進める
        automaticPresenceSimulation: true,
      },
    });

    await page.goto("/tasks");
    await loginViaLocal(page);

    // パスキー登録: /account画面の「パスキーを登録」ボタン
    await page.goto("/account");
    await page.getByTestId("passkey-register-button").click();
    await expect(page.getByTestId("passkey-register-success")).toBeVisible();

    // 一度ログアウトし、パスキーのみ(Keycloak・パスワード不要)でログインできることを検証する
    await page.getByTestId("logout-button").click();
    await expect(page.getByTestId("login-email-input")).toBeVisible({ timeout: 15_000 });

    await page.getByTestId("login-passkey-button").click();
    await expect(page.getByTestId("task-list")).toBeVisible();
  });
});
