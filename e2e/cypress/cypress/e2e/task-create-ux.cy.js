// Task登録UXの3パターン(inline/modal/page、CONTRACT.mdセクション19・19.7)のうち、inline版は既存のtask-crud.cy.jsでカバー済み
// ここではmodal版・page版を検証する
//
// フォーム自体の data-testid(task-name-input等)は inline/modal/page の3パターンで完全に共通(e2e/SELECTORS.md参照)
// 事前に MySQL 側で frontend.task-create-ux を modal / page に切り替えてから実行する
//
// 【実機検証で発覚した罠】TaskListModal.tsxのモーダルは、閉じる際にCSSで隠すのではなく
// showModal state を false にして `{showModal && (...)}` ごとDOMから完全にアンマウントする実装
//
// そのため閉じた後の状態は`.should("not.be.visible")`(要素はDOMに存在するが不可視、を期待する)
// ではなく`.should("not.exist")`(要素自体がDOMに無い、を期待する)で検証する必要がある
// (前者だと cy.get() 自体が「要素が見つからない」で即失敗し、想定と違うエラーメッセージで原因が分かりにくい)
describe("Task登録UX: モーダル版(frontend.task-create-ux=modal)", () => {
  beforeEach(() => {
    cy.visit("/tasks");
    cy.loginViaKeycloak();
  });

  it("「タスクを登録」ボタン→モーダルで作成→一覧に反映→モーダルで編集→一覧に反映", () => {
    const name = `MODAL${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    cy.getByTestId("task-create-button").click();
    cy.getByTestId("task-create-modal").should("be.visible");

    cy.getByTestId("task-name-input").type(name);
    cy.getByTestId("task-status-select").select("waiting");
    cy.getByTestId("task-finished-on-input").type("2030-01-01");
    cy.getByTestId("task-submit-button").click();

    cy.getByTestId("task-create-modal").should("not.exist");
    cy.getByTestId("task-row").contains(name).should("be.visible");

    cy.getByTestId("task-row")
      .contains(name)
      .parents('[data-testid="task-row"]')
      .find('[data-testid="task-edit-button"]')
      .click();
    cy.getByTestId("task-create-modal").should("be.visible");
    cy.getByTestId("task-name-input").clear().type(updatedName);
    cy.getByTestId("task-submit-button").click();
    cy.getByTestId("task-create-modal").should("not.exist");
    cy.getByTestId("task-row").contains(updatedName).should("be.visible");
  });

  it("モーダルは閉じるボタンでキャンセルでき、一覧はそのまま残る", () => {
    cy.getByTestId("task-create-button").click();
    cy.getByTestId("task-create-modal").should("be.visible");
    cy.getByTestId("task-modal-close").click();
    cy.getByTestId("task-create-modal").should("not.exist");
    cy.getByTestId("task-list").should("be.visible");
  });
});

describe("Task登録UX: 別ページ版(frontend.task-create-ux=page)", () => {
  beforeEach(() => {
    cy.visit("/tasks");
    cy.loginViaKeycloak();
  });

  it("「タスクを登録」リンク→別ページで作成→一覧へ戻る→一覧に反映", () => {
    const name = `PAGE${Date.now() % 100000}`;

    cy.getByTestId("task-create-link").click();
    cy.location("pathname").should("match", /\/tasks\/new$/);
    cy.getByTestId("task-form-page").should("be.visible");

    cy.getByTestId("task-name-input").type(name);
    cy.getByTestId("task-status-select").select("waiting");
    cy.getByTestId("task-finished-on-input").type("2030-01-01");
    cy.getByTestId("task-submit-button").click();

    cy.location("pathname").should("match", /\/tasks$/);
    cy.getByTestId("task-row").contains(name).should("be.visible");
  });

  it("編集ページへ遷移して更新→一覧へ戻る", () => {
    const name = `PAGE${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    cy.getByTestId("task-create-link").click();
    cy.getByTestId("task-name-input").type(name);
    cy.getByTestId("task-status-select").select("waiting");
    cy.getByTestId("task-finished-on-input").type("2030-01-01");
    cy.getByTestId("task-submit-button").click();
    cy.location("pathname").should("match", /\/tasks$/);

    cy.getByTestId("task-row")
      .contains(name)
      .parents('[data-testid="task-row"]')
      .find('[data-testid="task-edit-button"]')
      .click();
    cy.location("pathname").should("match", /\/tasks\/\d+\/edit$/);
    cy.getByTestId("task-name-input").clear().type(updatedName);
    cy.getByTestId("task-submit-button").click();
    cy.location("pathname").should("match", /\/tasks$/);
    cy.getByTestId("task-row").contains(updatedName).should("be.visible");
  });

  it("「一覧へ戻る」リンクで、保存せずに一覧へ戻れる(戻る導線の確認)", () => {
    cy.getByTestId("task-create-link").click();
    cy.getByTestId("task-form-page").should("be.visible");
    cy.getByTestId("back-to-tasks-link").click();
    cy.location("pathname").should("match", /\/tasks$/);
    cy.getByTestId("task-list").should("be.visible");
  });
});
