const assert = require("assert");
const { By, until, Key } = require("selenium-webdriver");
const { Select } = require("selenium-webdriver/lib/select");
const { buildDriver, testId, loginViaKeycloak, BASE_URL } = require("./helpers");

// <input type="date">へのsendKeysはブラウザのロケール表示(en-USなら mm/dd/yyyy 順の
// キー入力が必要)に依存して壊れやすい既知の問題(実機のSelenium実行で判明)
// PlaywrightのfillやCypressのtypeはこの差異を吸収してくれるが、素のWebDriverでは
// 自前でJS経由により値を設定する必要がある
//
// 【実機検証で判明した2段目の罠】単純に `element.value = X` を代入して
// input/changeイベントをdispatchするだけでは、DOM上の表示(value属性)は
// 変わるのにReactの内部state(制御コンポーネント)には反映されず、
// 送信されるJSONが空文字のままになっていた(React 16以降はinput要素の
// ネイティブvalue setterを自前のトラッキング付きsetterで上書きしており、
// 直接代入するとReact側の変更検知をすり抜けてしまうため)
// React公式にも知られたテスト手法である「ネイティブのvalue setterを
// Object.getOwnPropertyDescriptor経由で取得し、そのsetterを直接呼び出してから
// イベントをdispatchする」方法で回避する
async function setDateValue(driver, element, value) {
  await driver.executeScript(
    "const el = arguments[0];" +
      "const nativeSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;" +
      "nativeSetter.call(el, arguments[1]);" +
      "el.dispatchEvent(new Event('input', { bubbles: true }));" +
      "el.dispatchEvent(new Event('change', { bubbles: true }));",
    element,
    value
  );
}

describe("Task CRUD", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaKeycloak(driver);
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("作成→一覧に反映→更新→一覧に反映→削除→一覧から消える", async function () {
    const name = `E2E${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    // /tasks上部に常設されたフォームへ直接入力する(別画面遷移はない)
    await driver.wait(until.elementLocated(testId("task-name-input")), 10000);
    await driver.findElement(testId("task-name-input")).sendKeys(name);
    // <select>のoptionを直接clickするのは、ドロップダウンを開かずにクリックする
    // ことになり headless Chrome + WebDriverの組み合わせで不安定(changeイベントが発火しないことがある)
    // selenium-webdriver純正のSelectヘルパーを使う
    await new Select(await driver.findElement(testId("task-status-select"))).selectByValue(
      "waiting"
    );
    await setDateValue(driver, await driver.findElement(testId("task-finished-on-input")), "2030-01-01");
    await driver.findElement(testId("task-submit-button")).click();

    await driver.wait(until.elementLocated(By.xpath(`//*[@data-testid="task-row" and contains(., "${name}")]`)), 10000);

    // 更新
    const row = await driver.findElement(By.xpath(`//*[@data-testid="task-row" and contains(., "${name}")]`));
    // 行の編集ボタンを押すと上部フォームに値が入る(別画面遷移はない)
    await row.findElement(testId("task-edit-button")).click();
    const nameInput = await driver.wait(until.elementLocated(testId("task-name-input")), 10000);
    await nameInput.sendKeys(Key.chord(Key.CONTROL, "a"), Key.DELETE);
    await nameInput.sendKeys(updatedName);
    await driver.findElement(testId("task-submit-button")).click();
    await driver.wait(
      until.elementLocated(By.xpath(`//*[@data-testid="task-row" and contains(., "${updatedName}")]`)),
      10000
    );

    // 削除(window.confirmはChromeDevTools経由で自動許可する)
    const updatedRow = await driver.findElement(
      By.xpath(`//*[@data-testid="task-row" and contains(., "${updatedName}")]`)
    );
    await driver.executeScript("window.confirm = () => true;");
    await updatedRow.findElement(testId("task-delete-button")).click();

    await driver.wait(async () => {
      const rows = await driver.findElements(
        By.xpath(`//*[@data-testid="task-row" and contains(., "${updatedName}")]`)
      );
      return rows.length === 0;
    }, 10000);

    const remaining = await driver.findElements(
      By.xpath(`//*[@data-testid="task-row" and contains(., "${updatedName}")]`)
    );
    assert.strictEqual(remaining.length, 0);
  });
});
