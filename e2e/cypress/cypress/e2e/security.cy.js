// security.cy.js は e2e/playwright/tests/security.spec.ts の4シナリオをCypressへ移植したもの
// (3回目のe2e監査、セキュリティ観点)
// 今回は4シナリオともCypress標準APIだけで実装できた
// (WebAuthn仮想認証器・複数タブのような、Cypress側に構造的な制約がある機能を使わないため)
describe("セキュリティ(セッション・CSRF・XSS)", () => {
  it("session_id Cookieを別の値に書き換えると、以後のAPI呼び出しは401になる", () => {
    // 【なぜこのテストか】playwright版と同じ理由(CONTRACT.mdセクション2): ブラウザはbffが
    // 発行したセッションCookieしか持たず、これを改ざんしても他人になりすませないことを確認する
    cy.visit("/tasks");
    cy.loginViaLocal();
    cy.getByTestId("task-list").should("be.visible");

    cy.getCookie("session_id").then((cookie) => {
      expect(cookie).to.not.be.null;
      // 【cy.setCookieの挙動】Cypressはドメイン/パス等の他の属性を引き継がないため、
      // 元のCookieと同じpathを明示して上書きする(既定pathが違うと別Cookieとして扱われ、
      // 元のCookieが残ったまま=改ざんの検証にならない事故を避ける)
      cy.setCookie("session_id", "tampered-" + Math.random().toString(36).slice(2), {
        path: cookie.path,
      });
    });

    cy.window()
      .then((win) => win.fetch("/api/tasks", { credentials: "include" }))
      .then((res) => {
        expect(res.status).to.eq(401);
      });
  });

  it("CSRFトークンヘッダを付けずに状態変更リクエストを送ると403で拒否される", () => {
    cy.visit("/tasks");
    cy.loginViaLocal();
    cy.getByTestId("task-list").should("be.visible");

    cy.window()
      .then((win) =>
        win
          .fetch("/api/tasks", {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              name: "csrf-test",
              status: "waiting",
              finished_on: "2030-01-01",
              label_ids: [],
            }),
          })
          .then(async (res) => ({ status: res.status, body: await res.json() }))
      )
      .then((result) => {
        expect(result.status).to.eq(403);
        expect(result.body.error).to.contain("csrf");
      });
  });

  it("タスク名にscriptタグを含む文字列を入力しても、実行されずエスケープ表示される", () => {
    // 【playwright版との違い】
    // Cypress の window:alert イベントはブラウザの alert/confirm/prompt 全てを拾う
    // 削除確認 dialog も window:confirm として別途扱われるため、
    // alert 発火のカウントだけを見れば、削除操作の confirm と混同する心配が無い
    // (playwright 版で必要だった dialogCount のリセット処理が不要)
    let alertFired = false;
    cy.on("window:alert", () => {
      alertFired = true;
    });
    cy.on("window:confirm", () => true);

    cy.visit("/tasks");
    cy.loginViaLocal();
    cy.getByTestId("task-list").should("be.visible");

    const payload = "<script>x</script>";
    // 前回失敗分の残骸(同名行)があれば先に消しておく(playwright版と同じ理由、20文字制限で
    // ペイロードを一意化できないため)
    cy.get("body").then(($body) => {
      const staleRows = $body.find('[data-testid="task-row"]:contains("script")');
      if (staleRows.length > 0) {
        cy.wrap(staleRows).each(() => {
          cy.contains('[data-testid="task-row"]', "script")
            .find('[data-testid="task-delete-button"]')
            .click();
        });
      }
    });

    cy.getByTestId("task-name-input").type(payload);
    cy.getByTestId("task-status-select").select("waiting");
    cy.getByTestId("task-finished-on-input").type("2030-01-01");
    cy.getByTestId("task-submit-button").click();

    cy.contains('[data-testid="task-row"]', "script").should("be.visible");
    // alertが一度も発火していないこと(XSSが実行されていないこと)
    cy.wrap(null).should(() => {
      expect(alertFired).to.eq(false);
    });
    // テキストとして正しくエスケープ表示されていること
    cy.contains('[data-testid="task-row"]', payload).should("exist");
    // DOM上に実際の<script>要素が挿入されていないこと
    cy.contains('[data-testid="task-row"]', "script").find("script").should("not.exist");

    // 後片付け
    cy.contains('[data-testid="task-row"]', "script")
      .find('[data-testid="task-delete-button"]')
      .click();
    cy.contains('[data-testid="task-row"]', payload).should("not.exist");
  });

  it("ログアウト後、/tasksへの直接アクセスはログイン画面に戻される(bfcacheの生データ露出が無いことの確認)", () => {
    cy.visit("/tasks");
    cy.loginViaLocal();
    cy.getByTestId("task-list").should("be.visible");

    cy.getByTestId("logout-button").click();
    cy.getByTestId("login-email-input", { timeout: 15000 }).should("be.visible");

    cy.visit("/tasks");
    cy.getByTestId("login-email-input").should("be.visible");
    cy.getByTestId("task-list").should("not.exist");
  });
});
