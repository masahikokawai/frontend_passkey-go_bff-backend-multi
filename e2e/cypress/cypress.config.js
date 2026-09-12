const { defineConfig } = require("cypress");

module.exports = defineConfig({
  e2e: {
    baseUrl: process.env.FRONTEND_BASE_URL || "http://localhost:5173",
    supportFile: "cypress/support/e2e.js",
    // KeycloakへのクロスオリジンリダイレクトをCypressが素直に追従できるようにする
    // BFFパターンではKeycloakのログイン画面へ実際に302遷移するため、
    // Cypressの既定(同一オリジン制限)のままだと失敗する
    chromeWebSecurity: false,
  },
});
