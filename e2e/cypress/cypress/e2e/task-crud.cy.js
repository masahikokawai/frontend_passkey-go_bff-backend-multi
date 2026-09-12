describe("Task CRUD", () => {
  beforeEach(() => {
    cy.visit("/tasks");
    cy.loginViaKeycloak();
  });

  it("作成→一覧に反映→更新→一覧に反映→削除→一覧から消える", () => {
    const name = `E2E${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    // /tasks上部に常設されたフォームへ直接入力する(別画面遷移はない)
    cy.getByTestId("task-name-input").type(name);
    cy.getByTestId("task-status-select").select("waiting");
    cy.getByTestId("task-finished-on-input").type("2030-01-01");
    cy.getByTestId("task-submit-button").click();

    cy.getByTestId("task-row").contains(name).should("be.visible");

    // 行の編集ボタンを押すと上部フォームに値が入る(別画面遷移はない)
    cy.getByTestId("task-row")
      .contains(name)
      .parents('[data-testid="task-row"]')
      .find('[data-testid="task-edit-button"]')
      .click();
    cy.getByTestId("task-name-input").clear().type(updatedName);
    cy.getByTestId("task-submit-button").click();
    cy.getByTestId("task-row").contains(updatedName).should("be.visible");

    cy.on("window:confirm", () => true);
    cy.getByTestId("task-row")
      .contains(updatedName)
      .parents('[data-testid="task-row"]')
      .find('[data-testid="task-delete-button"]')
      .click();
    // 削除後は該当行自体が無くなる(0件)ため、cy.getByTestId(...).contains(...)のように
    // 先に要素集合を取得してから絞り込む形だと、絞り込み対象が空集合になった時点で
    // .contains()自身がタイムアウトして失敗し、後続の.should("not.exist")まで到達しない
    // (削除自体は成功しているのに見かけ上テストが落ちる、というCypressの既知のアンチパターン)
    //
    // cy.contains(selector, text)という「1コマンドで存在有無を問い合わせる」形にすることで、
    // 「そもそも見つからない」を正しく not.exist として判定できる
    cy.contains('[data-testid="task-row"]', updatedName).should("not.exist");
  });
});
