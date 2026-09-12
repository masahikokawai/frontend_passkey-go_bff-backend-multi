import { test, expect } from "@playwright/test";
import { loginViaLocal, uniqueTaskName } from "./helpers";

// 3回目のe2e監査(セキュリティ観点)で追加
// CONTRACT.mdセクション2(CSRF/セッション設計)を実際のブラウザ操作から裏付ける
// いずれも「壊れていないことの確認」であり、意図的に無効な入力を送って正しく拒否されることを見るテスト
test.describe("セキュリティ(セッション・CSRF・XSS)", () => {
  test("session_id Cookieを別の値に書き換えると、以後のAPI呼び出しは401になる", async ({
    page,
    context,
  }) => {
    // 【なぜこのテストか】
    // BFFパターンの前提(CONTRACT.mdセクション2)は「ブラウザは JWT を一切持たず、bff が発行したセッションCookieしか持たない」こと
    // このCookie自体が十分に推測困難で、かつ改ざんしても他人になりすませないことを確認する
    await page.goto("/tasks");
    await loginViaLocal(page);
    await expect(page.getByTestId("task-list")).toBeVisible();

    const cookies = await context.cookies();
    const sessionCookie = cookies.find((c) => c.name === "session_id");
    expect(sessionCookie).toBeTruthy();

    // 実在しないランダムなsession_idに書き換える(他人のセッションへの推測攻撃を模する)
    await context.addCookies([
      {
        ...sessionCookie!,
        value: "tampered-" + Math.random().toString(36).slice(2),
      },
    ]);

    // 書き換え後にAPIを呼ぶと、Redisにそのsession_idのキーが存在しないため401になるはず
    const status = await page.evaluate(async () => {
      const res = await fetch("/api/tasks", { credentials: "include" });
      return res.status;
    });
    expect(status).toBe(401);
  });

  test("CSRFトークンヘッダを付けずに状態変更リクエストを送ると403で拒否される", async ({
    page,
  }) => {
    // 【なぜこのテストか】bff/internal/auth/csrf.go の Double Submit Cookie 実装は
    // 「csrf_token Cookieの値とX-CSRF-Tokenヘッダの値が一致すること」を要求する
    // (CONTRACT.mdセクション2)
    // frontend実装(apiFetch)が正しくヘッダを付けていることに依存せず、
    // ヘッダを意図的に欠落させたfetchを直接発行して、bff側のミドルウェアが単体で正しく機能していることを確認する
    await page.goto("/tasks");
    await loginViaLocal(page);
    await expect(page.getByTestId("task-list")).toBeVisible();

    const result = await page.evaluate(async () => {
      // X-CSRF-Tokenヘッダを意図的に付けない(csrf_token Cookie自体はブラウザから
      // 自動送信されるが、ヘッダとの突き合わせが無いと拒否されるはず)
      const res = await fetch("/api/tasks", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: "csrf-test",
          status: "waiting",
          finished_on: "2030-01-01",
          label_ids: [],
        }),
      });
      return { status: res.status, body: await res.json() };
    });

    expect(result.status).toBe(403);
    expect(result.body.error).toContain("csrf");
  });

  test("タスク名にscriptタグを含む文字列を入力しても、実行されずエスケープ表示される", async ({
    page,
  }) => {
    // 【なぜこのテストか】Reactの{name}のようなJSX式は自動的にHTMLエスケープするため
    // dangerouslySetInnerHTML等を使わない限りXSSは起きないはずだが、これは
    // 「フロント実装がReactの挙動に依存しているだけ」の話であり、実際のブラウザ描画結果
    // (DOM)まで見て初めて裏付けになる
    // alert等のダイアログが実際に発火しないことと、生のタグ文字列がテキストとしてエスケープ表示される(HTMLとして解釈されない)ことの両方を見る
    // Task.nameは20文字以内のバリデーションがあるため、ペイロードは短いものを使う
    // (<script>x</script> は18文字)
    await page.goto("/tasks");
    await loginViaLocal(page);
    await expect(page.getByTestId("task-list")).toBeVisible();

    // 【実装時に判明】
    // このtest内で発生しうるdialogは2種類ある:
    // (a) XSSペイロードが万一実行された場合のalert等(万一にも発火してはいけない)、
    // (b) 後片付けの削除確認 dialog(これは正規のUI操作でありacceptしてよい)
    // 両方を単一のpage.on("dialog", ...)で受け止めてacceptしつつ、
    // 削除操作より前に1回でもdialogが発生していないかを dialogCountForXSSCheck で区別して確認する
    // (page.onは複数回発火しうるため、deleteをクリックする直前の値をスナップショットしてから比較する)
    let dialogCount = 0;
    page.on("dialog", (dialog) => {
      dialogCount++;
      void dialog.accept();
    });

    // 【実装時に判明】ペイロードはTask.nameの20文字制限により固定文字列
    // "<script>x</script>"にせざるを得ず、テスト失敗時の残骸(前回落ちた際に削除まで到達しなかった行)と同名になり得る
    // 実行前に同名の残骸が無いか確認し、あれば先に消しておくことで、strict modeのロケータ複数マッチ事故を避ける
    const payload = "<script>x</script>";
    const staleRows = page.getByTestId("task-row").filter({ hasText: "script" });
    const staleCount = await staleRows.count();
    for (let i = 0; i < staleCount; i++) {
      await staleRows.first().getByTestId("task-delete-button").click();
      await expect(staleRows).toHaveCount(staleCount - i - 1);
    }
    // 残骸削除で発生したdialogはこのテストの本題(XSS未発火の確認)には無関係なので、
    // ここでカウントをリセットしてから本題の作成操作に入る
    dialogCount = 0;
    await page.getByTestId("task-name-input").fill(payload);
    await page.getByTestId("task-status-select").selectOption("waiting");
    await page.getByTestId("task-finished-on-input").fill("2030-01-01");
    await page.getByTestId("task-submit-button").click();

    const row = page.getByTestId("task-row").filter({ hasText: "script" });
    await expect(row).toBeVisible();

    // 生のタグとしてDOMに挿入されていれば <script> 要素そのものは実行されないが
    // (innerHTML経由でも script タグは実行されない、というブラウザの既知の仕様がある)、
    // 万一 onerror 等の別ベクタで発火していないかを、削除操作(正規のconfirm dialog)より
    // 前の時点でdialogが1件も発生していないことで確認する
    expect(dialogCount).toBe(0);

    // テキストとして正しくエスケープされている(表示上そのまま文字列が見える)ことを確認
    await expect(row).toContainText(payload);
    // DOM上に実際の<script>要素として挿入されていないこと(ブラウザに解釈されていないこと)
    const scriptElementCount = await row.locator("script").count();
    expect(scriptElementCount).toBe(0);

    // 後片付け(削除確認dialogは上のpage.onハンドラが自動acceptする)
    // 【実装時に判明】
    // 保持していた row ロケータをそのまま使うと、
    // 直前の assertion 群の間に起きた再描画と競合し「element was detached from the DOM」で失敗することがあった
    // task-crud.spec.tsと同様、削除直前に改めてpage起点でロケータを組み立て直すと安定する
    await page
      .getByTestId("task-row")
      .filter({ hasText: "script" })
      .getByTestId("task-delete-button")
      .click();
    await expect(page.getByTestId("task-row").filter({ hasText: "script" })).toHaveCount(0);
  });

  test("ログアウト後、/tasksへの直接アクセスはログイン画面に戻される(bfcacheの生データ露出が無いことの確認)", async ({
    page,
  }) => {
    // 【なぜこのテストか】ログアウト直後にブラウザの「戻る」を押した際、ブラウザの
    // back-forward cache(bfcache)からタスク一覧が一瞬でも再描画されてしまわないかを確認する
    // bffのレスポンスにはCache-Control: no-store等の明示的な指定が無いため
    // (実装確認済み)、理論上はSPAのJS実行こそ再開されない可能性があるが、実際のブラウザ操作で「保護ページの内容が見えてしまわないか」を直接確認する
    await page.goto("/tasks");
    await loginViaLocal(page);
    await expect(page.getByTestId("task-list")).toBeVisible();

    await page.getByTestId("logout-button").click();
    await expect(page.getByTestId("login-email-input")).toBeVisible({ timeout: 15_000 });

    // ログアウト後に/tasksへ再度アクセス(ブラウザの戻るボタンではなくgotoだが、
    // 「保護ページへ再訪した際に確実にログイン画面へ戻されるか」という
    // セキュリティ上の本質は同じ)
    await page.goto("/tasks");
    await expect(page.getByTestId("login-email-input")).toBeVisible();
    // タスク一覧(ログイン前のセッションで見えていたはずのデータ)が
    // 一切表示されていないことも確認する
    await expect(page.getByTestId("task-list")).toHaveCount(0);
  });
});
