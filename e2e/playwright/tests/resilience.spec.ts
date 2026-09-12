import { test, expect } from "@playwright/test";
import { loginViaLocal } from "./helpers";

// 【2回目のe2e監査で追加】1回目の監査ではパスキー・admin画面という「機能追加への追随」に
// 集中していたため、今回は「壊れやすいのに見落とされがちな、アプリの土台部分の挙動」を
// 対象にする(ネットワーク遅延・ブラウザ操作・複数タブ・認証手段の後方互換)

test.describe("ネットワーク遅延時の挙動", () => {
  test("/api/tasksの応答が遅い間、「読み込み中...」が表示される", async ({ page }) => {
    // 【なぜ page.route での人為的遅延か】実際に遅いネットワークを用意するのは再現性が低い
    // Playwrightのroute interceptで実在のAPI呼び出しに遅延だけ
    // 上乗せすれば、backend/bffの実挙動はそのままに「遅い時どう見えるか」だけ検証できる
    await page.route("**/api/tasks*", async (route) => {
      await new Promise((r) => setTimeout(r, 1500));
      await route.continue();
    });

    await page.goto("/tasks");
    await loginViaLocal(page);
    // loginViaLocal内で既にtask-listの表示を待っているため、ここでは
    // ログイン直後の「読み込み中...」がその前段で一瞬でも出ていたことをネットワークログ側で
    // 保証するのが理想だが、フロントエンドはログイン成功→リダイレクト→再度/api/tasksを
    // 呼ぶ実装のため、リダイレクト後の画面遷移中に読み込み中が見えるかを確認する
    await page.reload();
    await page.route("**/api/tasks*", async (route) => {
      await new Promise((r) => setTimeout(r, 1500));
      await route.continue();
    });
    await page.goto("/tasks");
    await expect(page.getByText("読み込み中...")).toBeVisible();
    await expect(page.getByTestId("task-list")).toBeVisible({ timeout: 10_000 });
  });
});

test.describe("ブラウザバック・リロード", () => {
  test("タスク一覧→ラベル一覧→戻る、で状態が崩れず認証も維持される", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaLocal(page);

    await page.goto("/labels");
    await expect(page).toHaveURL(/\/labels/);

    await page.goBack();
    await expect(page).toHaveURL(/\/tasks/);
    // 戻った後も再ログインを要求されず、一覧がそのまま表示されることを確認する
    // (SPAのクライアントサイドルーティングだけでなく、ブラウザの戻る操作でも
    // 認証状態・データ取得が正しく再実行されるかの確認)
    await expect(page.getByTestId("task-list")).toBeVisible();
  });

  test("タスク一覧をリロードしても再ログインを要求されない(セッションCookieが効いている)", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaLocal(page);

    await page.reload();
    await expect(page.getByTestId("task-list")).toBeVisible();
    // ログイン画面へ飛ばされていないことも明示的に確認する
    await expect(page.getByTestId("login-email-input")).toHaveCount(0);
  });
});

test.describe("複数タブでのセッション共有", () => {
  test("片方のタブでログインすると、同じCookieを共有するもう片方のタブでも認証済み扱いになる", async ({ context }) => {
    const tab1 = await context.newPage();
    await tab1.goto("/tasks");
    await loginViaLocal(tab1);

    // 同じブラウザコンテキスト(=同じCookie jar)で新しいタブを開く
    // 実際のブラウザで「別タブで既にログイン済みのサイトを開く」ときの挙動を模している
    const tab2 = await context.newPage();
    await tab2.goto("/tasks");
    await expect(tab2.getByTestId("task-list")).toBeVisible();

    await tab1.close();
    await tab2.close();
  });

  test("片方のタブでログアウトすると、もう片方のタブは次のアクセスでログイン画面に戻る", async ({ context }) => {
    const tab1 = await context.newPage();
    await tab1.goto("/tasks");
    await loginViaLocal(tab1);

    const tab2 = await context.newPage();
    await tab2.goto("/tasks");
    await expect(tab2.getByTestId("task-list")).toBeVisible();

    // tab1でログアウト(bff側のRedisセッションが削除される)
    await tab1.getByTestId("logout-button").click();
    await expect(tab1.getByTestId("login-email-input")).toBeVisible();

    // tab2は開きっぱなしで何もしていないが、リロードして初めてサーバー側の
    // セッション失効に気づく、という一般的なSPAの挙動を確認する
    // (tab2はまだ古い画面を表示したままである可能性があるため、明示的にreloadする)
    await tab2.reload();
    await expect(tab2.getByTestId("login-email-input")).toBeVisible({ timeout: 15_000 });

    await tab1.close();
    await tab2.close();
  });
});

test.describe("パスキー登録後もパスワード認証が引き続き使える(回帰確認)", () => {
  test("パスキーを登録したユーザーが、パスワードでもログインできる", async ({ page }) => {
    // 【なぜこの確認が要るか】パスキー機能追加時、ログインルーティングやセッション発行の
    // 共通コードに手が入っている(auth_mode="passkey"の分岐追加等)
    // 既存のパスワードログインの経路を壊していないことを、パスキー登録「済み」の状態から改めて検証する
    // (未登録ユーザーでのパスワードログインは既存auth.spec.tsで確認済みのため、
    // ここでは「パスキー登録後も」という新しい組み合わせだけを対象にする)
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

    await page.goto("/tasks");
    await loginViaLocal(page);

    await page.goto("/account");
    await page.getByTestId("passkey-register-button").click();
    await expect(page.getByTestId("passkey-register-success")).toBeVisible();

    await page.getByTestId("logout-button").click();
    await expect(page.getByTestId("login-email-input")).toBeVisible({ timeout: 15_000 });

    // ここが今回の本題: パスキーボタンではなく、通常通りメールアドレス + パスワードでログインし直す
    // パスキー登録がこの経路を壊していないことを確認する
    await loginViaLocal(page);
    await expect(page.getByTestId("task-list")).toBeVisible();
  });
});
