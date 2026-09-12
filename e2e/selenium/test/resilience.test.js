const assert = require("assert");
const { until, By } = require("selenium-webdriver");
const { VirtualAuthenticatorOptions, Protocol, Transport } = require("selenium-webdriver/lib/virtual_authenticator");
const { buildDriver, testId, loginViaLocal, BASE_URL } = require("./helpers");

// resilience.test.js は e2e/playwright/tests/resilience.spec.ts の4シナリオを Selenium で実装したもの(2回目のe2e監査で追加)
//
// 「壊れやすいのに見落とされがちな、アプリの土台部分の挙動」(ネットワーク遅延・ブラウザ操作・複数タブ・認証手段の後方互換)を対象にする
describe("ネットワーク遅延時の挙動", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("/api/tasksの応答が遅い間、「読み込み中...」が表示される", async function () {
    // 【なぜCDP接続を使うか】selenium-webdriver には Playwright の page.route()のような高レベルのリクエスト傍受APIが無い
    //
    // Selenium 4はChromeに対してCDPコマンドを直接
    // 実行できる driver.createCDPConnection() を持っており、chromedp/go-rod と同じ
    // CDPのFetchドメインを使えば同じ「特定URLだけ遅延させる」ことができる
    //
    // 【実機検証で判明】
    // このバージョンのselenium-webdriverのCDPConnectionは、
    // 特定コマンドの応答を待つ`send()`と、応答を待たない`execute()`しか公開しておらず、
    // 「Fetch.requestPausedのような非同期イベントを継続的に受け取る」ための`.on()`のような
    // 公開APIが無い(ソース: node_modules/selenium-webdriver/devtools/CDPConnection.js)
    //
    // 内部で保持している`_wsConnection`(生のWebSocket接続)は公開プロパティではないが
    // アクセス自体は可能なため、そこへ直接`on('message', ...)`することでイベント購読を実現する
    const cdpConnection = await driver.createCDPConnection("page");
    await cdpConnection.send("Fetch.enable", {
      patterns: [{ urlPattern: "*/api/tasks*" }],
    });
    cdpConnection._wsConnection.on("message", (data) => {
      let payload;
      try {
        payload = JSON.parse(data.toString());
      } catch (e) {
        return;
      }
      if (payload.method !== "Fetch.requestPaused") return;
      setTimeout(() => {
        cdpConnection.execute("Fetch.continueRequest", { requestId: payload.params.requestId });
      }, 1500);
    });

    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);

    // ログイン成功→リダイレクト後、再度/api/tasksを呼ぶ実装のため、遅延を維持したままリロードし、
    // その瞬間に「読み込み中...」が見えることを確認する(JS版と同じ考え方)
    await driver.navigate().refresh();
    await driver.wait(until.elementLocated(By.xpath("//*[contains(text(), '読み込み中')]")), 5000);
    await driver.wait(until.elementLocated(testId("task-list")), 10000);
  });
});

describe("ブラウザバック・リロード", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("タスク一覧→ラベル一覧→戻る、で状態が崩れず認証も維持される", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);

    await driver.get(`${BASE_URL}/labels`);
    assert.ok((await driver.getCurrentUrl()).includes("/labels"));

    await driver.navigate().back();
    await driver.wait(until.elementLocated(testId("task-list")), 15000);
  });

  it("タスク一覧をリロードしても再ログインを要求されない(セッションCookieが効いている)", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);

    await driver.navigate().refresh();
    await driver.wait(until.elementLocated(testId("task-list")), 10000);
    const loginInputs = await driver.findElements(testId("login-email-input"));
    assert.strictEqual(loginInputs.length, 0, "リロード後にログイン画面へ飛ばされている");
  });
});

describe("複数タブでのセッション共有", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("片方のタブでログインすると、同じCookieを共有するもう片方のタブでも認証済み扱いになる", async function () {
    const tab1 = await driver.getWindowHandle();

    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);

    // Selenium 4の switchTo().newWindow("tab") で、同じブラウザセッション(=同じCookie jar)に
    // 新しいタブを開く(Playwright版のcontext.newPage()に相当)
    await driver.switchTo().newWindow("tab");
    await driver.get(`${BASE_URL}/tasks`);
    await driver.wait(until.elementLocated(testId("task-list")), 10000);

    await driver.close();
    await driver.switchTo().window(tab1);
  });

  it("片方のタブでログアウトすると、もう片方のタブは次のアクセスでログイン画面に戻る", async function () {
    const tab1 = await driver.getWindowHandle();
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);

    await driver.switchTo().newWindow("tab");
    const tab2 = await driver.getWindowHandle();
    await driver.get(`${BASE_URL}/tasks`);
    await driver.wait(until.elementLocated(testId("task-list")), 10000);

    // tab1でログアウト(bff側のRedisセッションが削除される)
    await driver.switchTo().window(tab1);
    await driver.findElement(testId("logout-button")).click();
    await driver.wait(until.elementLocated(testId("login-email-input")), 15000);

    // tab2はリロードして初めてサーバー側のセッション失効に気づく
    await driver.switchTo().window(tab2);
    await driver.navigate().refresh();
    await driver.wait(until.elementLocated(testId("login-email-input")), 15000);
  });
});

describe("パスキー登録後もパスワード認証が引き続き使える(回帰確認)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("パスキーを登録したユーザーが、パスワードでもログインできる", async function () {
    const options = new VirtualAuthenticatorOptions();
    options.setProtocol(Protocol.CTAP2);
    options.setTransport(Transport.INTERNAL);
    options.setHasResidentKey(true);
    options.setHasUserVerification(true);
    options.setIsUserVerified(true);
    await driver.addVirtualAuthenticator(options);

    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);

    await driver.get(`${BASE_URL}/account`);
    await driver.wait(until.elementLocated(testId("passkey-register-button")), 10000);
    await driver.findElement(testId("passkey-register-button")).click();
    await driver.wait(until.elementLocated(testId("passkey-register-success")), 10000);

    await driver.findElement(testId("logout-button")).click();
    await driver.wait(until.elementLocated(testId("login-email-input")), 15000);

    // ここが本題: パスキーボタンではなく、通常通りメールアドレス+パスワードでログインし直す
    await loginViaLocal(driver);
    const list = await driver.findElement(testId("task-list"));
    assert.ok(await list.isDisplayed());
  });
});
