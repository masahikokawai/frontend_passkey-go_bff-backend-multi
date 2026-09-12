const assert = require("assert");
const { By, until } = require("selenium-webdriver");
const { buildDriver, testId, loginViaKeycloak, loginViaLocal, BASE_URL } = require("./helpers");

describe("認証(BFFパターン)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("未ログイン状態で/tasksへアクセスするとローカルログイン画面(HMAC版・既定)へリダイレクトされる", async function () {
    // /tasksは保護ルート
    // useAuthが叩く/api/meの401をapiFetchが検知し、/login?redirect=... へ遷移する
    // (Keycloakへの自動遷移はしない CONTRACT.md セクション16)
    await driver.get(`${BASE_URL}/tasks`);
    await driver.wait(until.elementLocated(testId("login-email-input")), 10000);
    const el = await driver.findElement(testId("login-email-input"));
    assert.ok(await el.isDisplayed());
  });

  it("ローカル認証(HMAC版・既定)でログイン→タスク一覧表示→ログアウト→ログイン画面に戻る", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);
    const list = await driver.findElement(testId("task-list"));
    assert.ok(await list.isDisplayed());

    await driver.findElement(testId("logout-button")).click();
    await driver.wait(until.elementLocated(testId("login-email-input")), 15000);
  });

  it("ローカル認証(RSA版)でログイン→タスク一覧表示", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver, true);
    const list = await driver.findElement(testId("task-list"));
    assert.ok(await list.isDisplayed());
  });

  it("誤ったパスワードでのログインはエラーメッセージを表示し、ログイン画面に留まる", async function () {
    await driver.get(`${BASE_URL}/login`);
    await driver.wait(until.elementLocated(testId("login-email-input")), 10000);
    await driver.findElement(testId("login-email-input")).sendKeys("local-user@example.com");
    await driver.findElement(testId("login-password-input")).sendKeys("wrong-password");
    await driver.findElement(testId("login-submit-button")).click();

    await driver.wait(until.elementLocated(testId("login-error")), 10000);
    const emailInput = await driver.findElement(testId("login-email-input"));
    assert.ok(await emailInput.isDisplayed());
  });

  it("Keycloakでログイン→タスク一覧表示→ログアウト→ログイン画面に戻る", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaKeycloak(driver);
    const list = await driver.findElement(testId("task-list"));
    assert.ok(await list.isDisplayed());

    // ログアウト後は、
    // bff の /api/auth/logout(非同期fetch) →
    // Keycloak の end_session_endpointへの実遷移 →
    // post_logout_redirect_uriで5173へ戻る →
    // ProtectedLayoutが未ログインを検知し /login へ自動的に遷移する
    // という複数段の自動リダイレクトが発生する(Playwright実行時に判明した挙動と同じ)
    //
    // ここで明示的にdriver.get()すると、この自動リダイレクトの途中に割り込んでしまい不安定になるため、
    // クリック後は一連のリダイレクトが収束した最終状態
    // (ローカルログイン画面)を十分な猶予時間で待つだけにする
    await driver.findElement(testId("logout-button")).click();
    await driver.wait(until.elementLocated(testId("login-email-input")), 15000);
  });
});
