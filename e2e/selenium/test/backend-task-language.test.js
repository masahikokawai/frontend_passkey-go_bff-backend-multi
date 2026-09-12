const assert = require("assert");
const { By, until, Key } = require("selenium-webdriver");
const { Select } = require("selenium-webdriver/lib/select");
const { buildDriver, testId, loginViaKeycloak, BASE_URL, restoreFeatureFlagViaAdmin } = require("./helpers");

// CONTRACT.mdセクション20(backend多言語比較)の
// 「backend.task-languageをrustに切り替えても、Task CRUDがGoと同じ挙動で動く」ことを検証する
//
// これまでランブックの手順に沿ったcurl+目視、およびchromedp/go-rod/playwrightでのみ
// 自動確認されており、Cypress/Seleniumにはこの観点のテストが1つも無かった
// (e2e/cypress・e2e/selenium配下でtask-language/task_languageを検索してもヒット無し)
//
// task-create-ux.test.jsと同じ規約: このプロジェクトのSeleniumにはMySQLクライアントの
// 依存が無いため、DB側のflag自動切り替えは行わない
// 事前に MySQL 側で backend.task-language を rust に切り替え、
// backend-rust(追加構成、既定では未起動。CONTRACT.mdセクション20.9参照)を
// 起動してから実行すること
//
// setDateValueはtask-crud.test.jsと全く同じ回避策(React制御コンポーネントの既知の罠)
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

describe("Task CRUD: backend.task-language=rust(backend多言語比較)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaKeycloak(driver);
  });

  // 【e2eのfeature flagクリーンアップ信頼性監査で追加】このテスト自身はflagを切り替えない
  // (手動での事前切り替え運用のため)が、テストがassertion失敗で終わった場合でも
  // backend.task-languageが"rust"のまま残らないよう、既定値"go"へ戻すことだけは自動化する
  // (afterEachはit本体が失敗しても実行される、helpers.jsのrestoreFeatureFlagViaAdmin参照)
  afterEach(async function () {
    await restoreFeatureFlagViaAdmin("backend.task-language", "go");
    await driver.quit();
  });

  it("作成→一覧に反映→更新→一覧に反映→削除→一覧から消える(task-crud.test.jsと全く同じ操作、backendの実装言語だけが違う)", async function () {
    const name = `RUST${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    await driver.wait(until.elementLocated(testId("task-name-input")), 10000);
    await driver.findElement(testId("task-name-input")).sendKeys(name);
    await new Select(await driver.findElement(testId("task-status-select"))).selectByValue("waiting");
    await setDateValue(driver, await driver.findElement(testId("task-finished-on-input")), "2030-01-01");
    await driver.findElement(testId("task-submit-button")).click();

    await driver.wait(until.elementLocated(By.xpath(`//*[@data-testid="task-row" and contains(., "${name}")]`)), 10000);

    const row = await driver.findElement(By.xpath(`//*[@data-testid="task-row" and contains(., "${name}")]`));
    await row.findElement(testId("task-edit-button")).click();
    const nameInput = await driver.wait(until.elementLocated(testId("task-name-input")), 10000);
    await nameInput.sendKeys(Key.chord(Key.CONTROL, "a"), Key.DELETE);
    await nameInput.sendKeys(updatedName);
    await driver.findElement(testId("task-submit-button")).click();
    await driver.wait(
      until.elementLocated(By.xpath(`//*[@data-testid="task-row" and contains(., "${updatedName}")]`)),
      10000
    );

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
