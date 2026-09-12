// CONTRACT.mdセクション20(backend多言語比較)の
// 「backend.task-languageをrustに切り替えても、Task CRUDがGoと同じ挙動で動く」ことを検証する
//
// これまでランブックの手順に沿ったcurl+目視、およびchromedp/go-rod/playwrightでのみ
// 自動確認されており、Cypress/Seleniumにはこの観点のテストが1つも無かった
// (e2e/cypress・e2e/selenium配下でtask-language/task_languageを検索してもヒット無し)
//
// task-create-ux.cy.jsと同じ規約: このプロジェクトのCypressにはMySQLクライアントの
// 依存が無いため、DB側のflag自動切り替えは行わない
// 事前に MySQL 側で backend.task-language を rust に切り替え、
// backend-rust(追加構成、既定では未起動。CONTRACT.mdセクション20.9参照)を
// 起動してから実行すること
describe("Task CRUD: backend.task-language=rust(backend多言語比較)", () => {
  beforeEach(() => {
    cy.visit("/tasks");
    cy.loginViaKeycloak();
  });

  // 【e2eのfeature flagクリーンアップ信頼性監査で追加】このテスト自身はflagを切り替えない
  // (手動での事前切り替え運用のため)が、テストがassertion失敗で終わった場合でも
  // backend.task-languageが"rust"のまま残らないよう、既定値"go"へ戻すことだけは自動化する
  // (afterEachはit本体が失敗しても実行される、support/e2e.jsのrestoreFeatureFlagViaAdmin参照)
  afterEach(() => {
    cy.restoreFeatureFlagViaAdmin("backend.task-language", "go");
  });

  it("作成→一覧に反映→更新→一覧に反映→削除→一覧から消える(task-crud.cy.jsと全く同じ操作、backendの実装言語だけが違う)", () => {
    const name = `RUST${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    cy.getByTestId("task-name-input").type(name);
    cy.getByTestId("task-status-select").select("waiting");
    cy.getByTestId("task-finished-on-input").type("2030-01-01");
    cy.getByTestId("task-submit-button").click();

    cy.getByTestId("task-row").contains(name).should("be.visible");

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
    // task-crud.cy.jsの同名コメント参照: 空集合になる絞り込みではなく
    // cy.contains(selector, text)の1コマンド形で存在有無を問い合わせる
    cy.contains('[data-testid="task-row"]', updatedName).should("not.exist");
  });
});
