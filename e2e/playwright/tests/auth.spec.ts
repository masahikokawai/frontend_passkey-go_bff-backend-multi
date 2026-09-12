import { test, expect } from "@playwright/test";
import { loginViaKeycloak, loginViaLocal } from "./helpers";

test.describe("認証(BFFパターン)", () => {
  test("未ログイン状態で/tasksへアクセスするとローカルログイン画面(HMAC版・既定)へリダイレクトされる", async ({
    page,
  }) => {
    // /tasksは保護ルート
    // useAuthが叩く/api/meの401をapiFetchが検知し、
    // /login?redirect=... へ遷移する(Keycloakへの自動遷移はしない。CONTRACT.md セクション16)
    await page.goto("/tasks");
    await expect(page.getByTestId("login-email-input")).toBeVisible();
  });

  test("ローカル認証(HMAC版・既定)でログイン→タスク一覧表示→ログアウト→ログイン画面に戻る", async ({
    page,
  }) => {
    await page.goto("/tasks");
    await loginViaLocal(page);
    await expect(page.getByTestId("task-list")).toBeVisible();

    // ローカルセッションのログアウトはKeycloakのRP-Initiated Logoutを経由せず、
    // bffがFrontendBaseURLへの相対パスをそのまま返す(CONTRACT.md セクション16.4)
    // window.location.hrefでの遷移後、未ログイン状態の/がuseAuthの401検知で
    // 再度/loginへリダイレクトされる、という2段の自動遷移になる
    await page.getByTestId("logout-button").click();
    await expect(page.getByTestId("login-email-input")).toBeVisible({ timeout: 15_000 });
  });

  test("ローカル認証(RSA版)でログイン→タスク一覧表示", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaLocal(page, true);
    await expect(page.getByTestId("task-list")).toBeVisible();
  });

  test("誤ったパスワードでのログインはエラーメッセージを表示し、ログイン画面に留まる", async ({
    page,
  }) => {
    await page.goto("/login");
    await page.getByTestId("login-email-input").fill("local-user@example.com");
    await page.getByTestId("login-password-input").fill("wrong-password");
    await page.getByTestId("login-submit-button").click();

    await expect(page.getByTestId("login-error")).toBeVisible();
    await expect(page.getByTestId("login-email-input")).toBeVisible();
  });

  test("Keycloakでログイン→タスク一覧表示→ログアウト→ログイン画面に戻る", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);
    await expect(page.getByTestId("task-list")).toBeVisible();

    // ログアウト後の遷移は複数段ある: bffの/api/auth/logout(fetch)
    // → window.location.hrefでKeycloakのend_session_endpointへ実遷移(SSOセッション終了)
    // → post_logout_redirect_uriで http://localhost:5173/ へ戻る
    // → ProtectedLayoutが未ログインを検知し /login へ自動的にリダイレクトされる
    //   (「/」もログイン必須ルートのため)
    // 【実機のPlaywright実行で判明】この一連の流れは複数回のnavigationにまたがるため、
    // 途中の特定URL(end_session_endpoint通過時点など)をwaitForURLで待つと、
    // その後さらに自動リダイレクトが続いている最中に次のアクション(page.goto等)が
    // 割り込んでしまい、ログイン画面に正しくたどり着けないことがある
    // そのため明示的なgoto/中間URLの待機はせず、一連の自動リダイレクトが収束した
    // 最終状態(ローカルログイン画面)が見えることだけを、十分な猶予時間で待つ
    await page.getByTestId("logout-button").click();
    await expect(page.getByTestId("login-email-input")).toBeVisible({ timeout: 15_000 });
  });
});
