import { test, expect } from "@playwright/test";
import { loginViaKeycloak, uniqueTaskName, restoreFeatureFlagViaAdmin } from "./helpers";

// CONTRACT.mdセクション20(backend多言語比較)の「backend.task-languageをrustに
// 切り替えても、Task CRUDがGoと同じ挙動で動く」ことを検証する。これまでランブックの
// 手順に沿ったcurl+目視でのみ確認されており、自動テストが1つも無かった
// (e2e/以下でtask-language/task_languageを検索してもヒット無し)。
//
// task-create-ux.spec.ts(同じディレクトリ)と同じ規約: このプロジェクトのJS版e2eは
// feature flagを自己完結で切り替えず、事前にMySQL側で切り替えてから実行する運用にしている
// (Go版のe2e(e2e/chromedp・e2e/go-rod)はdatabase/sql経由で自己完結して切り替えるが、
// playwright(JS)側にはMySQLクライアントの依存が無いため、既存のtask-create-ux.spec.tsと
// 同じ「事前に手動/admin画面で切り替える」運用を踏襲する)
//
// また、backend-rustはCONTRACT.mdセクション20.9の「追加構成」であり既定では起動していない
// ため、このテストを実行する前に `cd backend-rust && cargo run` で起動しておくこと
//
//   事前に backend.task-language を admin画面等で "rust" に切り替え、
//   backend-rustを起動してから:
//   npx playwright test backend-task-language.spec.ts
//
//   確認後は backend.task-language を "go"(既定値)へ戻すこと
test.describe("backend多言語比較: backend.task-language=rust", () => {
  // 【e2eのfeature flagクリーンアップ信頼性監査で追加】このテスト自身はflagを切り替えない
  // (手動での事前切り替え運用のため)が、テストがassertion失敗で終わった場合でも
  // backend.task-languageが"rust"のまま残らないよう、既定値"go"へ戻すことだけは自動化する
  // (afterEachはtest本体が例外を投げても実行される、restoreFeatureFlagViaAdmin参照)
  test.afterEach(async ({ request }) => {
    await restoreFeatureFlagViaAdmin(request, "backend.task-language", "go");
  });

  test("Task CRUD(作成→一覧に反映→更新→一覧に反映→削除→一覧から消える)がGoと同じ挙動で動く", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);

    const name = uniqueTaskName("RUST");
    const updatedName = `${name}-upd`;

    // 作成(task-crud.spec.tsと全く同じ操作、backendの実装言語だけが違う)
    await page.getByTestId("task-name-input").fill(name);
    await page.getByTestId("task-status-select").selectOption("waiting");
    await page.getByTestId("task-finished-on-input").fill("2030-01-01");
    await page.getByTestId("task-submit-button").click();

    const row = page.getByTestId("task-row").filter({ hasText: name });
    await expect(row).toBeVisible();

    // 更新
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
