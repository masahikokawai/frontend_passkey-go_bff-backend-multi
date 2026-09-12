describe("認証(BFFパターン)", () => {
  it("未ログイン状態で/tasksへアクセスするとローカルログイン画面(HMAC版・既定)へリダイレクトされる", () => {
    cy.visit("/tasks");
    cy.getByTestId("login-email-input").should("be.visible");
  });

  it("ローカル認証(HMAC版・既定)でログイン→タスク一覧表示→ログアウト→ログイン画面に戻る", () => {
    cy.visit("/tasks");
    cy.loginViaLocal();
    cy.getByTestId("task-list").should("be.visible");

    cy.getByTestId("logout-button").click();
    cy.getByTestId("login-email-input", { timeout: 15000 }).should("be.visible");
  });

  it("ローカル認証(RSA版)でログイン→タスク一覧表示", () => {
    cy.visit("/tasks");
    cy.loginViaLocal(true);
    cy.getByTestId("task-list").should("be.visible");
  });

  it("誤ったパスワードでのログインはエラーメッセージを表示し、ログイン画面に留まる", () => {
    cy.visit("/login");
    cy.getByTestId("login-email-input").type("local-user@example.com");
    cy.getByTestId("login-password-input").type("wrong-password");
    cy.getByTestId("login-submit-button").click();

    cy.getByTestId("login-error").should("be.visible");
    cy.getByTestId("login-email-input").should("be.visible");
  });

  it("Keycloakでログイン→タスク一覧表示→ログアウト→ログイン画面に戻る", () => {
    cy.visit("/tasks");
    cy.loginViaKeycloak();
    cy.getByTestId("task-list").should("be.visible");

    cy.getByTestId("logout-button").click();
    cy.getByTestId("login-email-input", { timeout: 15000 }).should("be.visible");
  });
});
