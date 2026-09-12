import { Page } from "@playwright/test";

// frontendの /login は「ローカル認証(HMAC版・既定)のログインフォーム」であり、
// Keycloakへは自動遷移しない(CONTRACT.md セクション16)
// 未ログイン状態で保護ページへ goto すると、useAuthが叩く /api/me の401をapiFetchが検知し `/login?redirect=...` へ
// 遷移する(frontend/src/shared/api/client.ts参照)
// Keycloakでログインしたい場合は、このフォーム内の「Keycloakでログイン」ボタンを明示的にクリックする必要がある
export async function loginViaKeycloak(page: Page) {
  const username = process.env.E2E_USERNAME ?? "general-user";
  const password = process.env.E2E_PASSWORD ?? "password";

  await page.getByTestId("login-keycloak-button").waitFor({ state: "visible" });
  await page.getByTestId("login-keycloak-button").click();

  await page.locator("#username").waitFor({ state: "visible" });
  await page.locator("#username").fill(username);
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click();
  await page.getByTestId("task-list").waitFor({ state: "visible" });
}

// ローカル認証(HMAC版・RSA版共通)のログインフォームからのログイン
// (CONTRACT.md セクション16.1: /login はHMAC版、/login/rsa はRSA版
// フォーム自体は同じコンポーネント(LoginForm.tsx)を共用している)
export async function loginViaLocal(page: Page, rsa = false) {
  const email = process.env.E2E_LOCAL_EMAIL ?? "local-user@example.com";
  const password = process.env.E2E_LOCAL_PASSWORD ?? "password";

  if (rsa) {
    // /tasksへのgotoで既に/loginへリダイレクトされている状態から、RSA版へのリンクで遷移する
    await page.getByTestId("login-rsa-link").waitFor({ state: "visible" });
    await page.getByTestId("login-rsa-link").click();
  }

  await page.getByTestId("login-email-input").waitFor({ state: "visible" });
  await page.getByTestId("login-email-input").fill(email);
  await page.getByTestId("login-password-input").fill(password);
  await page.getByTestId("login-submit-button").click();
  await page.getByTestId("task-list").waitFor({ state: "visible" });
}

export function uniqueTaskName(prefix = "E2E"): string {
  // Task.nameは20文字以内のバリデーション(Rails/Go両方共通)のため短く保つ
  return `${prefix}${Date.now() % 100000}`;
}

// 【e2eのfeature flagクリーンアップ信頼性監査で追加】
// backend-task-language.spec.ts・backend-external-tasks-orm.spec.tsはこれまで
// 「事前に手動でMySQL/admin画面でflagを切り替えてから実行し、確認後は手動で戻す」という
// 完全手動の運用だった。これはtask-create-ux.spec.ts等、このプロジェクトのJS版e2e全体の
// 既存規約(MySQLクライアント依存を持たない)を踏襲したものだが、「テストがassertion失敗で
// 例外を投げた場合、手動での戻し忘れがそのまま残り、後続の無関係なテスト実行が
// 誤ったbackend言語/ORM実装に対して行われてしまう」という、Go版e2e
// (chromedp/go-rod/playwright-go、t.Cleanupで自動復元)には無いリスクが残っていた。
//
// admin/goにはflag_keyで直接更新するAPIが無い(POST /flags/:idは数値の内部id必須)ため、
// 新しい依存(mysql2等)を追加せずに自動復元するには、まずGET /(HTML)をBasic Auth付きで
// 取得し、対象のflag_keyの行からidを正規表現で抜き出してからPOSTする、という2段階が必要になる。
// admin/goのHTML(admin/go/web/templates/index.html)は
// `<code>{flag_key}</code>` の後に同じ<tr>内で `href="/flags/{id}/edit"` が続く構造が
// 安定しているため、この方式で新規npm依存無しに自動復元できる。
//
// これを使い、各テストのafterEachで「既定値へ戻す」処理を自動化する
// (このテスト自身はflagを切り替えないため、切り替え前の値を覚えておく必要は無く、
// 常にこのプロジェクトの既定値へ戻すだけでよい)
export async function restoreFeatureFlagViaAdmin(
  request: import("@playwright/test").APIRequestContext,
  flagKey: string,
  defaultValue: string
) {
  const adminBaseUrl = process.env.ADMIN_GO_BASE_URL ?? "http://localhost:8091";
  const adminUser = process.env.ADMIN_BASIC_AUTH_USER ?? "admin";
  const adminPassword = process.env.ADMIN_BASIC_AUTH_PASSWORD ?? "password";
  const authHeader = { Authorization: `Basic ${Buffer.from(`${adminUser}:${adminPassword}`).toString("base64")}` };

  const indexRes = await request.get(adminBaseUrl + "/", { headers: authHeader });
  if (!indexRes.ok()) return; // admin/go未起動等でも、この復元処理自体でテストを失敗させない
  const html = await indexRes.text();

  const escapedKey = flagKey.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = new RegExp(`<code>${escapedKey}</code>[\\s\\S]*?href="/flags/(\\d+)/edit"`).exec(html);
  if (!match) return;
  const id = match[1];

  // 【実装時に発見した実際の罠】admin/goのUpdateハンドラは`enabled`フォーム項目が
  // 無いと`false`(HTMLのchecked無しcheckbox仕様、feature_flag.go参照)として扱うため、
  // ここで明示的に"on"を送らないと、default_variationを戻すつもりが誤ってflag自体を
  // 無効化してしまう(このプロジェクトの全flagは常時enabled運用のため、これ自体が新たな
  // 事故になりかねない箇所だった)
  await request.post(`${adminBaseUrl}/flags/${id}`, {
    headers: { ...authHeader, "Content-Type": "application/x-www-form-urlencoded" },
    form: { enabled: "on", default_variation: defaultValue },
  });
}
