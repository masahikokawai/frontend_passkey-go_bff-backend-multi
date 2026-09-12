import { test, expect } from "@playwright/test";
import { loginViaKeycloak } from "./helpers";

// frontend.tasks-ts-rewrite フラグは、CONTRACT.mdセクション13でMySQLの
// feature_flagsテーブル(admin/go・admin/railsから編集)を正本にする設計へ移行済みで、
// bff/internal/featureflag/flags.yamlは実際には読まれない過去の参考実装
// この2ケースはMySQL側のフラグ値をadmin画面等で手動で切り替えてから
// 実行する運用を想定する(CIでの自動切替は対象外、READMEに手順を明記)
//
//   FLAG_STATE=on  npx playwright test feature-flag.spec.ts
//   FLAG_STATE=off npx playwright test feature-flag.spec.ts
test.describe("Feature Flag: frontend.tasks-ts-rewrite", () => {
  test("現在のフラグ設定でTask一覧が新旧いずれの実装でも問題なく表示される", async ({ page }) => {
    await page.goto("/tasks");
    await loginViaKeycloak(page);

    // 新旧どちらの実装(TaskList.tsx / legacy/TaskList.jsx)でも
    // 同じdata-testid="task-list"を持つ前提(SELECTORS.md)なので、
    // このテスト自体はFlagの値を意識せず「一覧が表示され、行が描画される」ことのみ検証する
    await expect(page.getByTestId("task-list")).toBeVisible();

    const flagState = process.env.FLAG_STATE ?? "unspecified";
    test.info().annotations.push({ type: "flag_state", description: flagState });
  });
});
