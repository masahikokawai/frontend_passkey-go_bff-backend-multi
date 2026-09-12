import { test, expect } from "@playwright/test";

// 【2回目のe2e監査で発見】admin/go(:8091)・admin/rails(:8092)は、これまで6フレームワーク
// いずれのe2eの対象にもなっていなかった(既存シナリオは全てfrontend+bffが対象)
//
// Feature Flag管理・ユーザー管理(パスキー登録状況の列を含む)という、実際に運用上重要な
// 画面が未検証のまま残っていたため、Playwrightに最低限のシナリオを追加する
//
// 両実装(admin/go・admin/rails)は同じMySQLテーブルを直接操作するだけの薄いCRUD画面で、
// テンプレートに data-testid が付与されていない(admin/* はこのフォークのスコープ外のため新規付与もできない)
// そのため getByRole/getByLabel 等、アクセシビリティツリーに基づく
// 選択子だけで両実装を検証する(data-testidに頼らない書き方の比較にもなる)
const ADMIN_APPS = [
  { name: "admin/go", baseURL: "http://localhost:8091" },
  { name: "admin/rails", baseURL: "http://localhost:8092" },
];

const ADMIN_USER = process.env.ADMIN_BASIC_AUTH_USER ?? "admin";
const ADMIN_PASSWORD = process.env.ADMIN_BASIC_AUTH_PASSWORD ?? "password";

// 「on/offの2値」に戻せることが確実な既存フラグを使う(多値フラグや他テストが依存する
// 値を触ると、並行実行中の他スイートに影響しうるため)
const TOGGLE_FLAG_KEY = "frontend.tasks-ts-rewrite";

for (const app of ADMIN_APPS) {
  test.describe(`${app.name} 管理画面`, () => {
    test("Basic Auth無しでアクセスすると401になる", async ({ request }) => {
      // test.use({ httpCredentials }) はdescribeブロック全体に効いてしまうため、
      // 無認証を確認したいこのテストだけ独立したAPIRequestContextで叩く
      const res = await request.get(app.baseURL + "/");
      expect(res.status()).toBe(401);
    });

    test.describe("認証済み操作", () => {
      test.use({ httpCredentials: { username: ADMIN_USER, password: ADMIN_PASSWORD } });

      test("Feature Flag一覧が表示され、on/offを切り替えると一覧に反映される", async ({ page }) => {
        await page.goto(app.baseURL + "/");
        await expect(page.locator("h1")).toContainText(/Feature Flag/);

        // 【実機検証で判明】admin/go・admin/railsはテーブル列順が同じ(flag_key/説明/有効/
        // デフォルト値/更新日時/操作)なので、列インデックスで値を読む方がgetByTextより堅牢
        //
        // 「デフォルト値」列の文字列自体(例: "on")が説明文の英単語に偶然一致し、
        // getByTextがstrict modeで複数要素にマッチしてしまう事故が実機テストで発覚したため
        const row = page.getByRole("row").filter({ hasText: TOGGLE_FLAG_KEY });
        await expect(row).toBeVisible();
        const defaultValueCell = row.locator("td").nth(3);
        const originalValue = (await defaultValueCell.textContent())?.trim();

        await row.getByRole("link", { name: "編集" }).click();
        // 【実機検証で判明】admin/goは<select name="default_variation">、admin/railsは
        // Railsのform_with(model:)がモデル名で名前空間化するため
        // <select name="feature_flag[default_variation]">になる
        // 両実装を同じテストで検証するため、どちらの命名にもマッチするセレクタにする
        const select = page.locator('select[name="default_variation"], select[name$="[default_variation]"]');
        const newValue = (await select.inputValue()) === "on" ? "off" : "on";
        await select.selectOption(newValue);
        await page.getByRole("button", { name: "保存" }).click();

        await expect(page.locator("h1")).toContainText(/Feature Flag/);
        const updatedRow = page.getByRole("row").filter({ hasText: TOGGLE_FLAG_KEY });
        await expect(updatedRow.locator("td").nth(3)).toHaveText(newValue);

        // 後片付け: 元の値に戻す
        await updatedRow.getByRole("link", { name: "編集" }).click();
        await page.locator('select[name="default_variation"], select[name$="[default_variation]"]').selectOption(originalValue!);
        await page.getByRole("button", { name: "保存" }).click();
      });

      test("Feature Flag変更履歴(audit log)画面が開ける", async ({ page }) => {
        await page.goto(app.baseURL + "/");
        const row = page.getByRole("row").filter({ hasText: TOGGLE_FLAG_KEY });
        await row.getByRole("link", { name: "変更履歴" }).click();
        // 見出し文言はgo/rails間で微妙に違う可能性があるため、URLベースで到達確認する
        await expect(page).toHaveURL(/audit_log/);
      });

      test("ユーザー一覧にパスキー列があり、ローカル認証ユーザーを新規作成できる", async ({ page }) => {
        await page.goto(app.baseURL + "/users");
        // 【実機検証で判明】admin/goの見出しは「ユーザー管理」、admin/railsは「ユーザー一覧」で
        // 文言が異なる上、ページ内に「新規作成」というh2見出しも別途あるため、
        // getByRole("heading", {name: /ユーザー/})はstrict modeで複数要素にマッチして落ちた
        // h1だけに絞ることで両実装かつ両見出し文言に対応できるようにする
        await expect(page.locator("h1")).toBeVisible();
        // CONTRACT.mdセクション22.7で追加したパスキー列
        // 値は「登録済み」/「未登録」のどちらかが必ずどこかの行に出る
        // (全ユーザーがパスキー未登録ということはこれまでの動作確認で無いはずだが、念のため「未登録」の存在だけを厳密条件にする)
        await expect(page.getByText("未登録").first()).toBeVisible();

        const uniqueEmail = `e2e-admin-${Date.now()}@example.com`;
        // 【実機検証で判明・既知の制約として記録】admin/goのテンプレートは<label>が
        // <input>と兄弟要素なだけでfor/id関連付けが無いため、getByLabelでは要素が
        // 見つからない(admin/goはこのフォークのスコープ外のためテンプレート修正はしない)
        // name属性ベースのロケータなら両実装で確実に一致するのでこちらを使う
        await page.locator('input[name="name"]').fill("E2E管理画面テスト");
        await page.locator('input[name="email"]').fill(uniqueEmail);
        await page.locator('input[name="password"]').fill("password123");
        await page.locator('form[action="/users"], form[action$="/users"]').getByRole("button", { name: /作成|Create/ }).click();

        await expect(page.getByText(uniqueEmail)).toBeVisible();

        // 後片付け: 作成したユーザーを削除する
        const newRow = page.getByRole("row").filter({ hasText: uniqueEmail });
        page.once("dialog", (d) => d.accept());
        await newRow.getByRole("button", { name: "削除" }).click();
        await expect(page.getByText(uniqueEmail)).toHaveCount(0);
      });
    });
  });
}
