import { test, expect } from "@playwright/test";
import { loginViaKeycloak, uniqueTaskName } from "./helpers";

test.describe("Task CRUD", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);
  });

  test("作成→一覧に反映→更新→一覧に反映→削除→一覧から消える", async ({ page }) => {
    const name = uniqueTaskName();
    const updatedName = `${name}-upd`;

    // 作成(/tasks上部に常設されたフォームへ直接入力する。別画面遷移はない)
    await page.getByTestId("task-name-input").fill(name);
    await page.getByTestId("task-status-select").selectOption("waiting");
    await page.getByTestId("task-finished-on-input").fill("2030-01-01");
    await page.getByTestId("task-submit-button").click();

    const row = page.getByTestId("task-row").filter({ hasText: name });
    await expect(row).toBeVisible();

    // 更新(行の編集ボタンを押すと上部フォームに値が入る。別画面遷移はない)
    await row.getByTestId("task-edit-button").click();
    await page.getByTestId("task-name-input").fill(updatedName);
    await page.getByTestId("task-submit-button").click();
    await expect(page.getByTestId("task-row").filter({ hasText: updatedName })).toBeVisible();

    // 削除
    page.once("dialog", (dialog) => dialog.accept());
    await page
      .getByTestId("task-row")
      .filter({ hasText: updatedName })
      .getByTestId("task-delete-button")
      .click();
    await expect(page.getByTestId("task-row").filter({ hasText: updatedName })).toHaveCount(0);
  });
});
