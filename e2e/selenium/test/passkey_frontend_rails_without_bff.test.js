const assert = require("assert");
const { By, until } = require("selenium-webdriver");
const { VirtualAuthenticatorOptions, Protocol, Transport } = require("selenium-webdriver/lib/virtual_authenticator");
const { buildDriver } = require("./helpers");

// frontend-rails/without-bff(CONTRACT.mdセクション21・22.9)のベースURL。
// React+bff(:5173、BASE_URL)とは別の独立したRailsアプリのため専用の環境変数を使う
const FRONTEND_RAILS_WITHOUT_BFF_BASE_URL =
  process.env.FRONTEND_RAILS_WITHOUT_BFF_BASE_URL || "http://localhost:5174";
const USERNAME = process.env.E2E_USERNAME || "general-user";
const PASSWORD = process.env.E2E_PASSWORD || "password";

// CONTRACT.mdセクション22.9(2026-09-11追記、frontend-rails/without-bffへのパスキー追加)
//
// 【なぜこのテストが必要か】このシナリオはランブックに「e2e未実装(コア構成のReact+bffフローのみが
// 対象)」と明記されていた既知のギャップだった。既存のpasskey.test.js(bffのローカル認証ユーザー向け、
// セクション22.1)とは全く別の対象(Keycloak発行ユーザー向けパスキー)を検証する。
//
// 【他のシナリオとの違い】このアプリはReactのdata-testid規約(e2e/SELECTORS.md)に従っておらず、
// 素のRails ERBビューに素朴なid属性のみを付与している(login.html.erb/account.html.erb参照)。
// そのためこのファイルだけ`By.css("#id")`・`By.xpath`によるテキスト一致を使う(`testId()`ヘルパーは
// このアプリには使えない、helpers.jsのtestIdは`[data-testid=...]`前提のため)
describe("frontend-rails/without-bff パスキー(WebAuthn)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("Keycloakでログイン→パスキー登録→ログアウト→パスキーのみでログイン", async function () {
    const options = new VirtualAuthenticatorOptions();
    options.setProtocol(Protocol.CTAP2);
    options.setTransport(Transport.INTERNAL);
    // hasResidentKey: discoverable credential(CONTRACT.mdセクション22.2)の検証に必須
    options.setHasResidentKey(true);
    options.setHasUserVerification(true);
    options.setIsUserVerified(true);
    await driver.addVirtualAuthenticator(options);

    await driver.get(`${FRONTEND_RAILS_WITHOUT_BFF_BASE_URL}/login`);

    // 「Keycloakでログイン」はRailsのbutton_to(POSTフォーム送信)で、id/data-testidを持たないため、
    // XPathのテキスト一致で要素を特定する(passkey_test.jsの核app版とは異なりtestId()は使えない)
    await driver.wait(until.elementLocated(By.xpath("//button[contains(., 'Keycloakでログイン')]")), 10000);
    await driver.findElement(By.xpath("//button[contains(., 'Keycloakでログイン')]")).click();

    await driver.wait(until.elementLocated(By.id("username")), 10000);
    await driver.findElement(By.id("username")).sendKeys(USERNAME);
    await driver.findElement(By.id("password")).sendKeys(PASSWORD);
    await driver.findElement(By.id("kc-login")).click();
    await driver.wait(until.urlContains("/welcome"), 15000);

    // パスキー登録: /account画面の「パスキーを登録」ボタン
    await driver.get(`${FRONTEND_RAILS_WITHOUT_BFF_BASE_URL}/account`);
    await driver.wait(until.elementLocated(By.css("#passkey-register-button")), 10000);
    await driver.findElement(By.css("#passkey-register-button")).click();
    await driver.wait(async () => {
      const status = await driver.findElement(By.css("#passkey-status")).getText();
      return status.includes("登録しました");
    }, 15000);

    // ログアウト(welcome.html.erbのボタンはbutton_to(DELETE)だが、config/routes.rbには
    // 「ブラウザから直接叩いての手動確認をしやすくするため」というコメント付きでGET /logoutも
    // 明示的に許可されている。E2Eでもこの経路をそのまま使う)
    await driver.get(`${FRONTEND_RAILS_WITHOUT_BFF_BASE_URL}/logout`);

    // ここから先はKeycloakを一切使わない、パスキーのみでのログイン
    await driver.get(`${FRONTEND_RAILS_WITHOUT_BFF_BASE_URL}/login`);
    await driver.wait(until.elementLocated(By.css("#passkey-login-button")), 10000);
    await driver.findElement(By.css("#passkey-login-button")).click();
    await driver.wait(until.urlContains("/welcome"), 15000);

    // welcome.html.erbは`ログイン方式: <%= session[:auth_mode] %>`をそのまま出力する
    const body = await driver.findElement(By.css("body")).getText();
    assert.ok(body.includes("ログイン方式: passkey"), `welcome画面に「ログイン方式: passkey」が表示されていない。本文: ${body}`);
  });
});
