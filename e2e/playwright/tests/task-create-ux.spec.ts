import { test, expect } from "@playwright/test";
import { loginViaKeycloak, uniqueTaskName } from "./helpers";

// Task登録UXの3パターン(inline/modal/page、CONTRACT.mdセクション19・19.7)のうち、
// inline版は既存のtask-crud.spec.tsでカバー済み
// ここではmodal版・page版を検証する
//
// フォーム自体のdata-testid(task-name-input等)はinline/modal/pageの3パターンで
// 完全に共通(e2e/SELECTORS.md参照)なので、「どの導線でフォームへたどり着くか」だけが異なる
// 事前にMySQL側でfrontend.task-create-uxを modal / page に切り替えてから実行する
//
//   FLAG_STATE=modal を admin画面等で反映してから:
//   npx playwright test task-create-ux.spec.ts -g "モーダル版"
//   FLAG_STATE=page を admin画面等で反映してから:
//   npx playwright test task-create-ux.spec.ts -g "別ページ版"
test.describe("Task登録UX: モーダル版(frontend.task-create-ux=modal)", () => {
  test("「タスクを登録」ボタン→モーダルで作成→一覧に反映→モーダルで編集→一覧に反映", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);

    const name = uniqueTaskName("MODAL");
    const updatedName = `${name}-upd`;

    // モーダル版ではinline版と違い、常設フォームの代わりに「タスクを登録」ボタンがある
    await page.getByTestId("task-create-button").click();
    await expect(page.getByTestId("task-create-modal")).toBeVisible();

    await page.getByTestId("task-name-input").fill(name);
    await page.getByTestId("task-status-select").selectOption("waiting");
    await page.getByTestId("task-finished-on-input").fill("2030-01-01");
    await page.getByTestId("task-submit-button").click();

    // 保存成功でモーダルが閉じ、一覧に反映される
    await expect(page.getByTestId("task-create-modal")).toBeHidden();
    const row = page.getByTestId("task-row").filter({ hasText: name });
    await expect(row).toBeVisible();

    // 編集も同じ導線でモーダルが開く
    await row.getByTestId("task-edit-button").click();
    await expect(page.getByTestId("task-create-modal")).toBeVisible();
    await page.getByTestId("task-name-input").fill(updatedName);
    await page.getByTestId("task-submit-button").click();
    await expect(page.getByTestId("task-create-modal")).toBeHidden();
    await expect(page.getByTestId("task-row").filter({ hasText: updatedName })).toBeVisible();
  });

  test("モーダルは閉じるボタンでキャンセルでき、一覧はそのまま残る", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);

    await page.getByTestId("task-create-button").click();
    await expect(page.getByTestId("task-create-modal")).toBeVisible();
    await page.getByTestId("task-modal-close").click();
    await expect(page.getByTestId("task-create-modal")).toBeHidden();
    // モーダルを閉じても一覧画面自体はそのまま表示され続けている
    await expect(page.getByTestId("task-list")).toBeVisible();
  });
});

test.describe("Task登録UX: 別ページ版(frontend.task-create-ux=page)", () => {
  test("「タスクを登録」リンク→別ページで作成→一覧へ戻る→一覧に反映", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);

    const name = uniqueTaskName("PAGE");

    await page.getByTestId("task-create-link").click();
    await expect(page).toHaveURL(/\/tasks\/new$/);
    await expect(page.getByTestId("task-form-page")).toBeVisible();

    await page.getByTestId("task-name-input").fill(name);
    await page.getByTestId("task-status-select").selectOption("waiting");
    await page.getByTestId("task-finished-on-input").fill("2030-01-01");
    await page.getByTestId("task-submit-button").click();

    // 保存成功で自動的に一覧(/tasks)へ戻る
    await expect(page).toHaveURL(/\/tasks$/);
    await expect(page.getByTestId("task-row").filter({ hasText: name })).toBeVisible();
  });

  test("編集ページへ遷移して更新→一覧へ戻る", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);

    const name = uniqueTaskName("PAGE");
    const updatedName = `${name}-upd`;

    await page.getByTestId("task-create-link").click();
    await page.getByTestId("task-name-input").fill(name);
    await page.getByTestId("task-status-select").selectOption("waiting");
    await page.getByTestId("task-finished-on-input").fill("2030-01-01");
    await page.getByTestId("task-submit-button").click();
    await expect(page).toHaveURL(/\/tasks$/);

    const row = page.getByTestId("task-row").filter({ hasText: name });
    await row.getByTestId("task-edit-button").click();
    await expect(page).toHaveURL(/\/tasks\/\d+\/edit$/);
    await page.getByTestId("task-name-input").fill(updatedName);
    await page.getByTestId("task-submit-button").click();
    await expect(page).toHaveURL(/\/tasks$/);
    await expect(page.getByTestId("task-row").filter({ hasText: updatedName })).toBeVisible();
  });

  test("「一覧へ戻る」リンクで、保存せずに一覧へ戻れる(戻る導線の確認)", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);

    await page.getByTestId("task-create-link").click();
    await expect(page.getByTestId("task-form-page")).toBeVisible();
    await page.getByTestId("back-to-tasks-link").click();
    await expect(page).toHaveURL(/\/tasks$/);
    await expect(page.getByTestId("task-list")).toBeVisible();
  });
});
