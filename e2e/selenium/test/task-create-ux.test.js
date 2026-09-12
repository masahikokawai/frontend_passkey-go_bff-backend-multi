const assert = require("assert");
const { By, until } = require("selenium-webdriver");
const { Select } = require("selenium-webdriver/lib/select");
const { buildDriver, testId, loginViaKeycloak, BASE_URL } = require("./helpers");

// Task登録UXの3パターン(inline/modal/page、CONTRACT.mdセクション19・19.7)のうち、inline版は既存のtask-crud.test.jsでカバー済み
// ここではmodal版・page版を検証する
//
// フォーム自体のdata-testid(task-name-input等)はinline/modal/pageの3パターンで完全に共通(e2e/SELECTORS.md参照)
// 事前にMySQL側でfrontend.task-create-uxをmodal / page に切り替えてから実行する
//
// <input type="date">への値設定はtask-crud.test.jsと同じネイティブsetter経由の回避策(setDateValue)が必要(React制御コンポーネントの既知の罠)
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

// setTextValue はsetDateValueと全く同じ仕組みだが、テキストinputの「値を丸ごと置き換える」用途で使う
//
// 【実機検証で発覚した罠】
// /tasks/:id/edit(React Router経由のクライアントサイド遷移)では、
// Key.chord(Key.CONTROL, "a"), Key.DELETE による選択→削除が効かなかった(inline版の編集
// (画面遷移を伴わず同じフォームDOMを使い回す)では同じ書き方が問題無く動いていたため、
// 遷移直後のフォーカス状態の違いに起因すると見られる)
// Ctrl+Aがinput内のテキストではなく
// ページ全体を選択してしまっていた可能性があり、その状態でDELETEを送っても入力欄の中身は
// 変わらず、直後のsendKeysで元の値の末尾に新しい値が連結されてしまっていた
// (例: "foo"+"foo-upd" → "foofoo-upd"、chromedp版で踏んだのと同種の罠)
//
// 日付inputで既に使っているネイティブsetter直接呼び出しの方式に統一することで解決した
async function setTextValue(driver, element, value) {
  await setDateValue(driver, element, value);
}

// 【実機検証で発覚した罠】モーダル版(task-create-modal)は独自のスクロールコンテナを持つため、
// ブラウザウィンドウが小さい(このプロジェクトのCI/ローカル既定サイズだと可視領域が414px程度しかない)場合、
// task-submit-buttonがビューポート外(可視領域の下)に配置されてしまう
//
// WebDriverの.click() は通常自動でスクロールしてくれるが、
// ネストしたスクロールコンテナの中までは追従しないため、
// 実際にはボタンの矩形は存在する(getBoundingClientRectで確認可能)のに
// "element not interactable"としてクリックが失敗する
// ネイティブのElement.scrollIntoView()は
// ネストしたスクロールコンテナも正しく辿るため、クリック前に明示的に呼び出すことで解決する
async function scrollIntoView(driver, element) {
  await driver.executeScript("arguments[0].scrollIntoView({ block: 'center' });", element);
}

function rowByText(text) {
  return By.xpath(`//*[@data-testid="task-row" and contains(., "${text}")]`);
}

describe("Task登録UX: モーダル版(frontend.task-create-ux=modal)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaKeycloak(driver);
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("「タスクを登録」ボタン → モーダルで作成 → 一覧に反映→モーダルで編集 → 一覧に反映", async function () {
    const name = `MODAL${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    await driver.wait(until.elementLocated(testId("task-create-button")), 10000);
    await driver.findElement(testId("task-create-button")).click();
    await driver.wait(until.elementLocated(testId("task-create-modal")), 10000);

    await driver.findElement(testId("task-name-input")).sendKeys(name);
    await new Select(await driver.findElement(testId("task-status-select"))).selectByValue("waiting");
    await setDateValue(driver, await driver.findElement(testId("task-finished-on-input")), "2030-01-01");
    const createSubmitBtn = await driver.findElement(testId("task-submit-button"));
    await scrollIntoView(driver, createSubmitBtn);
    await createSubmitBtn.click();

    await driver.wait(until.elementLocated(rowByText(name)), 10000);

    const row = await driver.findElement(rowByText(name));
    await row.findElement(testId("task-edit-button")).click();
    await driver.wait(until.elementLocated(testId("task-create-modal")), 10000);
    const nameInput = await driver.wait(until.elementLocated(testId("task-name-input")), 10000);
    // モーダル内のフォームも、開くたびに新しくマウントされる要素のため、
    // 上記(別ページ版)と同じ理由でsetTextValue(ネイティブsetter経由)を使う
    await driver.wait(async () => (await nameInput.getAttribute("value")) === name, 10000);
    await setTextValue(driver, nameInput, updatedName);
    const editSubmitBtn = await driver.findElement(testId("task-submit-button"));
    await scrollIntoView(driver, editSubmitBtn);
    await editSubmitBtn.click();

    await driver.wait(until.elementLocated(rowByText(updatedName)), 10000);
  });

  it("モーダルは閉じるボタンでキャンセルでき、一覧はそのまま残る", async function () {
    await driver.wait(until.elementLocated(testId("task-create-button")), 10000);
    await driver.findElement(testId("task-create-button")).click();
    await driver.wait(until.elementLocated(testId("task-create-modal")), 10000);
    await driver.findElement(testId("task-modal-close")).click();

    await driver.wait(async () => {
      const modals = await driver.findElements(testId("task-create-modal"));
      return modals.length === 0 || !(await modals[0].isDisplayed());
    }, 10000);
    const list = await driver.findElement(testId("task-list"));
    assert.strictEqual(await list.isDisplayed(), true);
  });
});

describe("Task登録UX: 別ページ版(frontend.task-create-ux=page)", function () {
  let driver;

  beforeEach(async function () {
    driver = await buildDriver();
    await driver.get(`${BASE_URL}/tasks`);
    await loginViaKeycloak(driver);
  });

  afterEach(async function () {
    await driver.quit();
  });

  it("「タスクを登録」リンク→別ページで作成 → 一覧へ戻る → 一覧に反映", async function () {
    const name = `PAGE${Date.now() % 100000}`;

    await driver.wait(until.elementLocated(testId("task-create-link")), 10000);
    await driver.findElement(testId("task-create-link")).click();
    await driver.wait(until.urlMatches(/\/tasks\/new$/), 10000);
    await driver.wait(until.elementLocated(testId("task-form-page")), 10000);

    await driver.findElement(testId("task-name-input")).sendKeys(name);
    await new Select(await driver.findElement(testId("task-status-select"))).selectByValue("waiting");
    await setDateValue(driver, await driver.findElement(testId("task-finished-on-input")), "2030-01-01");
    await driver.findElement(testId("task-submit-button")).click();

    await driver.wait(until.urlMatches(/\/tasks$/), 10000);
    await driver.wait(until.elementLocated(rowByText(name)), 10000);
  });

  it("編集ページへ遷移して更新 → 一覧へ戻る", async function () {
    const name = `PAGE${Date.now() % 100000}`;
    const updatedName = `${name}-upd`;

    await driver.wait(until.elementLocated(testId("task-create-link")), 10000);
    await driver.findElement(testId("task-create-link")).click();
    await driver.wait(until.elementLocated(testId("task-name-input")), 10000);
    await driver.findElement(testId("task-name-input")).sendKeys(name);
    await new Select(await driver.findElement(testId("task-status-select"))).selectByValue("waiting");
    await setDateValue(driver, await driver.findElement(testId("task-finished-on-input")), "2030-01-01");
    await driver.findElement(testId("task-submit-button")).click();
    await driver.wait(until.urlMatches(/\/tasks$/), 10000);

    const row = await driver.wait(until.elementLocated(rowByText(name)), 10000);
    await row.findElement(testId("task-edit-button")).click();
    await driver.wait(until.urlMatches(/\/tasks\/\d+\/edit$/), 10000);
    const nameInput = await driver.wait(until.elementLocated(testId("task-name-input")), 10000);
    // 【実機検証で発覚した競合】/tasks/:id/edit は、遷移直後は空のフォームをまず描画し、
    // 対象タスクをAPIから非同期に取得できた時点でReactの制御されたinputへ値を流し込む
    // (inline版の編集は一覧取得時に既にメモリ上にある値を即座に流し込むだけなので、この非同期な間が存在しない)
    //
    // そのため要素が見つかった直後にクリア+入力すると、
    // 直後に非同期取得が完了して元の値で上書きされ、せっかく入力した値が消えてしまう
    // (「更新したはずなのに元の名前のまま」という壊れ方をする)
    // 対象タスクの元の名前がinputに反映されるまで明示的に待ってから操作する
    await driver.wait(async () => (await nameInput.getAttribute("value")) === name, 10000);
    await setTextValue(driver, nameInput, updatedName);
    await driver.findElement(testId("task-submit-button")).click();

    await driver.wait(until.urlMatches(/\/tasks$/), 10000);
    await driver.wait(until.elementLocated(rowByText(updatedName)), 10000);
  });

  it("「一覧へ戻る」リンクで、保存せずに一覧へ戻れる(戻る導線の確認)", async function () {
    await driver.wait(until.elementLocated(testId("task-create-link")), 10000);
    await driver.findElement(testId("task-create-link")).click();
    await driver.wait(until.elementLocated(testId("task-form-page")), 10000);
    await driver.findElement(testId("back-to-tasks-link")).click();
    await driver.wait(until.urlMatches(/\/tasks$/), 10000);
    const list = await driver.wait(until.elementLocated(testId("task-list")), 10000);
    assert.strictEqual(await list.isDisplayed(), true);
  });
});
