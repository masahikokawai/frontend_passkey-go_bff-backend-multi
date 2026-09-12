const assert = require("assert");
const { By, until } = require("selenium-webdriver");
const { buildDriver, testId, loginViaLocal, BASE_URL } = require("./helpers");

// security.test.js は e2e/playwright/tests/security.spec.ts の4シナリオを Selenium へ移植したもの
//
// 4シナリオともselenium-webdriver標準API
// (Cookie操作・executeAsyncScriptでのfetch実行・window.alert/confirmの上書き)だけで実装できた
describe("セキュリティ(セッション・CSRF・XSS)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("session_id Cookieを別の値に書き換えると、以後のAPI呼び出しは401になる", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);
    await driver.wait(until.elementLocated(testId("task-list")), 15000);

    const cookie = await driver.manage().getCookie("session_id");
    assert.ok(cookie, "session_id Cookieが見つからない");

    // 【なぜdriver.manage().addCookieか】selenium-webdriverはCookieを名前で上書きする
    // (addCookieで同名のCookieを渡すと値が置き換わる、Playwrightのcontext.addCookies等と同じ挙動)
    await driver.manage().addCookie({
      name: "session_id",
      value: "tampered-" + Math.random().toString(36).slice(2),
      path: cookie.path,
    });

    // 【なぜexecuteAsyncScriptか】classic WebDriverのexecuteScriptはPromiseの解決を待たない
    // (返り値がPromiseそのものになってしまう)ため、fetchの完了を待つには
    // コールバック引数(arguments[arguments.length - 1])を呼ぶexecuteAsyncScriptを使う必要がある
    const status = await driver.executeAsyncScript((callback) => {
      fetch("/api/tasks", { credentials: "include" })
        .then((res) => callback(res.status))
        .catch(() => callback(-1));
    });
    assert.strictEqual(status, 401);
  });

  it("CSRFトークンヘッダを付けずに状態変更リクエストを送ると403で拒否される", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);
    await driver.wait(until.elementLocated(testId("task-list")), 15000);

    const result = await driver.executeAsyncScript((callback) => {
      fetch("/api/tasks", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: "csrf-test",
          status: "waiting",
          finished_on: "2030-01-01",
          label_ids: [],
        }),
      })
        .then((res) => res.json().then((body) => callback({ status: res.status, body })))
        .catch(() => callback({ status: -1, body: {} }));
    });

    assert.strictEqual(result.status, 403);
    assert.ok(String(result.body.error).includes("csrf"));
  });

  it("タスク名にscriptタグを含む文字列を入力しても、実行されずエスケープ表示される", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);
    await driver.wait(until.elementLocated(testId("task-list")), 15000);

    // 【なぜwindow.alertを上書きするか】selenium-webdriverはPlaywrightのpage.on("dialog")のような統一ダイアログイベントAPIを持たない
    // window.alertが実際に呼ばれたかどうかを
    // フラグとして記録し、後でexecuteScriptで読み出す方式にする
    // (window.confirmは削除確認dialogで正規に使われるため、常にtrueを返すよう別途上書きする)
    await driver.executeScript(() => {
      window.__xssAlertFired = false;
      window.alert = () => {
        window.__xssAlertFired = true;
      };
      window.confirm = () => true;
    });

    const payload = "<script>x</script>";
    // 前回失敗分の残骸(同名行)があれば先に消しておく(playwright版と同じ理由)
    let staleRows = await driver.findElements(
      By.xpath('//*[@data-testid="task-row" and contains(., "script")]')
    );
    for (let i = 0; i < staleRows.length; i++) {
      const rows = await driver.findElements(
        By.xpath('//*[@data-testid="task-row" and contains(., "script")]')
      );
      if (rows.length === 0) break;
      await rows[0].findElement(testId("task-delete-button")).click();
      await driver.sleep(300);
    }

    await driver.findElement(testId("task-name-input")).sendKeys(payload);
    const { Select } = require("selenium-webdriver/lib/select");
    await new Select(await driver.findElement(testId("task-status-select"))).selectByValue("waiting");
    await driver.executeScript((el, value) => {
      const nativeSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value").set;
      nativeSetter.call(el, value);
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
    }, await driver.findElement(testId("task-finished-on-input")), "2030-01-01");
    await driver.findElement(testId("task-submit-button")).click();

    const row = await driver.wait(
      until.elementLocated(By.xpath('//*[@data-testid="task-row" and contains(., "script")]')),
      10000
    );

    // alertが一度も発火していないこと(XSSが実行されていないこと)
    const alertFired = await driver.executeScript(() => window.__xssAlertFired);
    assert.strictEqual(alertFired, false);

    // テキストとして正しくエスケープ表示されていること
    assert.ok((await row.getText()).includes(payload));
    // DOM上に実際の<script>要素が挿入されていないこと
    const scriptElements = await row.findElements(By.css("script"));
    assert.strictEqual(scriptElements.length, 0);

    // 後片付け
    await row.findElement(testId("task-delete-button")).click();
    await driver.wait(async () => {
      const remaining = await driver.findElements(
        By.xpath('//*[@data-testid="task-row" and contains(., "script")]')
      );
      return remaining.length === 0;
    }, 10000);
  });

  it("ログアウト後、/tasksへの直接アクセスはログイン画面に戻される(bfcacheの生データ露出が無いことの確認)", async function () {
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaLocal(driver);
    await driver.wait(until.elementLocated(testId("task-list")), 15000);

    await driver.findElement(testId("logout-button")).click();
    await driver.wait(until.elementLocated(testId("login-email-input")), 15000);

    await driver.get(`${BASE_URL}/tasks`);
    await driver.wait(until.elementLocated(testId("login-email-input")), 10000);
    const taskLists = await driver.findElements(testId("task-list"));
    assert.strictEqual(taskLists.length, 0);
  });
});
