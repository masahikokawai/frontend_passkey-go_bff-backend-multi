// resilience.cy.js は e2e/playwright/tests/resilience.spec.ts のうち、Cypressで移植可能な
// シナリオを実装したもの(2回目のe2e監査で追加)
// 「壊れやすいのに見落とされがちな、アプリの土台部分の挙動」(ネットワーク遅延・ブラウザ操作)を対象にする
//
// 【移植しなかった2シナリオについて】
// - 複数タブでのセッション共有・ログアウト伝播: Cypressは仕様上、複数タブ/ウィンドウの
//   同時制御を公式サポートしていない(1回目監査のWebAuthn仮想認証器と同じ既知の制約)
// - パスキー登録後もパスワードログインが壊れていないことの回帰確認: このシナリオは
//   「実際にパスキーを登録する」ステップを前提にしており、既存passkey.spec.ts相当のテストが
//   CypressだけWebAuthn仮想認証器に対応していないため存在しない(1回目監査で判明済み)
//   このシナリオもパスキー登録ステップを必要とするため、同じ理由でCypressには移植できない
//   (この2点はcypress/README.mdにも記載する)

describe("ネットワーク遅延時の挙動", () => {
  it("/api/tasksの応答が遅い間、「読み込み中...」が表示される", () => {
    // 【なぜcy.interceptか】
    // Cypress の cy.intercept は Playwrightのpage.route()に相当する
    // 高レベルAPIで、レスポンスに直接delayオプションを指定できる
    // これが今回移植した4フレームワーク(chromedp/go-rod/playwright-go/Selenium)の中で最も簡潔に書けた実装
    cy.intercept("**/api/tasks*", (req) => {
      req.on("response", (res) => {
        res.setDelay(1500);
      });
    }).as("tasksDelayed");

    cy.visit("/tasks");
    cy.loginViaLocal();

    // ログイン成功→リダイレクト後、再度/api/tasksを呼ぶ実装のため、遅延を維持したままリロードし、
    // その瞬間に「読み込み中...」が見えることを確認する(JS版Playwrightと同じ考え方)
    cy.reload();
    cy.contains("読み込み中...").should("be.visible");
    cy.getByTestId("task-list", { timeout: 10000 }).should("be.visible");
  });
});

describe("ブラウザバック・リロード", () => {
  it("タスク一覧→ラベル一覧→戻る、で状態が崩れず認証も維持される", () => {
    cy.visit("/tasks");
    cy.loginViaLocal();

    cy.visit("/labels");
    cy.url().should("include", "/labels");

    cy.go("back");
    cy.getByTestId("task-list").should("be.visible");
  });

  it("タスク一覧をリロードしても再ログインを要求されない(セッションCookieが効いている)", () => {
    cy.visit("/tasks");
    cy.loginViaLocal();

    cy.reload();
    cy.getByTestId("task-list").should("be.visible");
    cy.getByTestId("login-email-input").should("not.exist");
  });
});
