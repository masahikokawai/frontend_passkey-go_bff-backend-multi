const assert = require("assert");
const { until } = require("selenium-webdriver");
const { VirtualAuthenticatorOptions, Protocol, Transport } = require("selenium-webdriver/lib/virtual_authenticator");
const { buildDriver, testId, loginViaLocal, BASE_URL } = require("./helpers");

// CONTRACT.mdセクション22(パスキー/WebAuthn)
// 対象はbffのローカル認証(HMAC/RSA)ユーザーのみ
// (Keycloak発行ユーザーは対象外、22.1参照)
//
// 【なぜこの実装にしたか】Selenium 4は他の5フレームワークと違い、CDPを直接叩くのではなく
// W3C WebDriver仕様自体が標準化した「Virtual Authenticator拡張」を使う
// (`driver.addVirtualAuthenticator(options)`、chromedriverが実装)
//
// これはChrome専用のCDPコマンドではなく
// 仕様上はどのWebDriver実装(Firefox等)でも将来的に同じAPIで動きうる、という点がCDP系3フレームワーク
// (chromedp/go-rod/playwright-go)+Playwright(JS)との対比になっている
// (実際にはchromedriverがCDPのWebAuthnドメインへ変換して実行しているが、テストコードからは見えない)
describe("パスキー(WebAuthn)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("ローカル認証(HMAC)でログイン→パスキー登録→ログアウト→パスキーのみでログイン", async function () {
    const options = new VirtualAuthenticatorOptions();
    options.setProtocol(Protocol.CTAP2);
    options.setTransport(Transport.INTERNAL);
    // hasResidentKey: discoverable credential(CONTRACT.mdセクション22.2)の検証に必須
    options.setHasResidentKey(true);
    options.setHasUserVerification(true);
    options.setIsUserVerified(true);
    // isUserConsentingは既定でtrueのため明示不要(CDP版のautomaticPresenceSimulationに相当し、
    // CIで人手のタッチ操作を待たせない)
    await driver.addVirtualAuthenticator(options);

    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);

    // パスキー登録: /account画面の「パスキーを登録」ボタン
    await driver.get(`${BASE_URL}/account`);
    await driver.wait(until.elementLocated(testId("passkey-register-button")), 10000);
    await driver.findElement(testId("passkey-register-button")).click();
    await driver.wait(until.elementLocated(testId("passkey-register-success")), 10000);

    // 一度ログアウトし、パスキーのみ(Keycloak・パスワード不要)でログインできることを検証する
    await driver.findElement(testId("logout-button")).click();
    await driver.wait(until.elementLocated(testId("login-email-input")), 15000);

    await driver.wait(until.elementLocated(testId("login-passkey-button")), 10000);
    await driver.findElement(testId("login-passkey-button")).click();
    await driver.wait(until.elementLocated(testId("task-list")), 10000);
    const list = await driver.findElement(testId("task-list"));
    assert.ok(await list.isDisplayed());
  });
});
