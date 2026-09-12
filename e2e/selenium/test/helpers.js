const { Builder, By, until } = require("selenium-webdriver");
const chrome = require("selenium-webdriver/chrome");

const BASE_URL = process.env.FRONTEND_BASE_URL || "http://localhost:5173";
const USERNAME = process.env.E2E_USERNAME || "general-user";
const PASSWORD = process.env.E2E_PASSWORD || "password";
const LOCAL_EMAIL = process.env.E2E_LOCAL_EMAIL || "local-user@example.com";
const LOCAL_PASSWORD = process.env.E2E_LOCAL_PASSWORD || "password";

// selenium-webdriverはW3C WebDriverプロトコルでブラウザを操作する
// PlaywrightやchromedpがブラウザのネイティブAPI(CDP)を直接叩くのに対し、
// SeleniumはWebDriverサーバ(chromedriver)を介す一段レイヤーが多い構成
async function buildDriver() {
  const options = new chrome.Options();
  if (process.env.HEADLESS !== "false") {
    options.addArguments("--headless=new");
  }
  return new Builder().forBrowser("chrome").setChromeOptions(options).build();
}

function testId(id) {
  return By.css(`[data-testid="${id}"]`);
}

// frontendの /login は「ローカル認証(HMAC版・既定)のログインフォーム」であり、 Keycloak へは自動遷移しない(CONTRACT.md セクション16)
// 呼び出し側が先に保護ページへdriver.get()した時点で、ブラウザは/api/meの401をapiFetchが検知し/loginへ遷移済みの
// はずなので、ここではさらに「Keycloakでログイン」ボタンをクリックしてから Keycloak 自身のログイン画面を操作する
async function loginViaKeycloak(driver) {
  await driver.wait(until.elementLocated(testId("login-keycloak-button")), 10000);
  await driver.findElement(testId("login-keycloak-button")).click();

  await driver.wait(until.elementLocated(By.id("username")), 10000);
  await driver.findElement(By.id("username")).sendKeys(USERNAME);
  await driver.findElement(By.id("password")).sendKeys(PASSWORD);
  await driver.findElement(By.id("kc-login")).click();
  await driver.wait(until.elementLocated(testId("task-list")), 15000);
}

// ローカル認証(HMAC版・RSA版共通)のログインフォームからのログイン
// (CONTRACT.md セクション16.1: /login はHMAC版、/login/rsa はRSA版)
async function loginViaLocal(driver, rsa = false) {
  if (rsa) {
    await driver.wait(until.elementLocated(testId("login-rsa-link")), 10000);
    await driver.findElement(testId("login-rsa-link")).click();
  }

  await driver.wait(until.elementLocated(testId("login-email-input")), 10000);
  await driver.findElement(testId("login-email-input")).sendKeys(LOCAL_EMAIL);
  await driver.findElement(testId("login-password-input")).sendKeys(LOCAL_PASSWORD);
  await driver.findElement(testId("login-submit-button")).click();
  await driver.wait(until.elementLocated(testId("task-list")), 15000);
}

// 【e2eのfeature flagクリーンアップ信頼性監査で追加】
// backend-task-language.test.js・backend-external-tasks-orm.test.jsはこれまで「事前に手動で
// MySQL/admin画面でflagを切り替えてから実行し、確認後は手動で戻す」という完全手動の運用だった。
// テストがassertion失敗で終わった場合、手動での戻し忘れがそのまま残り、後続の無関係な
// テスト実行が誤ったbackend言語/ORM実装に対して行われてしまうリスクがあった
// (Go版e2e(chromedp/go-rod/playwright-go)はt.Cleanupで自動復元しており、この差があった)。
//
// admin/goにはflag_keyで直接更新するAPIが無い(POST /flags/:idは数値の内部id必須)ため、
// 新しい依存を追加せずに自動復元するには、まずGET /(HTML)をBasic Auth付きで取得し、
// 対象のflag_keyの行からidを正規表現で抜き出してからPOSTする、という2段階が必要になる。
// admin/goのHTML(admin/go/web/templates/index.html)は`<code>{flag_key}</code>`の後に
// 同じ<tr>内で`href="/flags/{id}/edit"`が続く構造が安定しているため、この方式で
// 新規npm依存無し(Node組み込みのfetchのみ、backend-external-tasks-orm.test.jsと同じ流儀)で
// 自動復元できる
//
// 【実装時に発見した実際の罠】admin/goのUpdateハンドラは`enabled`フォーム項目が無いと
// `false`(HTMLのchecked無しcheckbox仕様)として扱うため、明示的に"on"を送らないと、
// default_variationを戻すつもりが誤ってflag自体を無効化してしまう
async function restoreFeatureFlagViaAdmin(flagKey, defaultValue) {
  const adminBaseUrl = process.env.ADMIN_GO_BASE_URL || "http://localhost:8091";
  const adminUser = process.env.ADMIN_BASIC_AUTH_USER || "admin";
  const adminPassword = process.env.ADMIN_BASIC_AUTH_PASSWORD || "password";
  const authHeader = { Authorization: `Basic ${Buffer.from(`${adminUser}:${adminPassword}`).toString("base64")}` };

  const indexRes = await fetch(adminBaseUrl + "/", { headers: authHeader });
  if (!indexRes.ok) return; // admin/go未起動等でも、この復元処理自体でテストを失敗させない
  const html = await indexRes.text();

  const escapedKey = flagKey.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = new RegExp(`<code>${escapedKey}</code>[\\s\\S]*?href="/flags/(\\d+)/edit"`).exec(html);
  if (!match) return;

  await fetch(`${adminBaseUrl}/flags/${match[1]}`, {
    method: "POST",
    headers: { ...authHeader, "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ enabled: "on", default_variation: defaultValue }),
  });
}

module.exports = { buildDriver, testId, loginViaKeycloak, loginViaLocal, BASE_URL, restoreFeatureFlagViaAdmin };
