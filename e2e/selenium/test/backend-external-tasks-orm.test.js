const assert = require("assert");

// CONTRACT.mdセクション24(backend(Go)内でのORM比較、GORM/bob)の
// 「backend.external-tasks-ormをbobに切り替えても、GORMと完全に同一のレスポンス形状で
// 応答する」ことを検証する
//
// backend_external_tasks_orm_test.go(chromedp)と同じ理由で、これはbackend(Go、コア構成)
// 自身が評価する軸でbff・frontendを一切経由しないため、ブラウザ操作は不要な
// 純粋なHTTPレベルのテストにしている(このファイルだけbuildDriver/BASE_URLを使わない)
//
// Node 18+ 組み込みのfetch(ブラウザ実行ではなくNode自身のグローバル)を使う。
// security.test.jsの既存パターンはブラウザ内fetch(driver.executeAsyncScript経由)だが、
// このテストはブラウザ操作自体が不要なため素のNode fetchで完結させる
//
// task-create-ux.test.jsと同じ規約: 事前に MySQL 側で backend.external-tasks-orm を
// bob に切り替えてから実行すること(このプロジェクトのSeleniumにはMySQLクライアントの
// 依存が無いため、DB側のflag自動切り替えは行わない)
const { restoreFeatureFlagViaAdmin } = require("./helpers");

describe("外部公開API: backend.external-tasks-orm=bob", function () {
  // 【e2eのfeature flagクリーンアップ信頼性監査で追加】backend-task-language.test.jsと
  // 同じ理由(assertion失敗時でも既定値へ確実に戻すため。このファイルはdriverを使わないため
  // driver.quit()は無い)
  afterEach(async function () {
    await restoreFeatureFlagViaAdmin("backend.external-tasks-orm", "gorm");
  });

  it("GET /external/v1/tasksがGORMと同一のレスポンス形状(offsetページング)で200を返す", async function () {
    const tokenRes = await fetch("http://localhost:8082/realms/training/protocol/openid-connect/token", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        grant_type: "client_credentials",
        client_id: "external-api-client",
        client_secret: "external-api-client-local-dev-secret",
      }),
    });
    assert.strictEqual(tokenRes.status, 200, "Keycloakのトークン取得に失敗");
    const { access_token: accessToken } = await tokenRes.json();
    assert.ok(accessToken, "access_tokenが空でない");

    const tasksRes = await fetch("http://localhost:8097/external/v1/tasks?user_id=1", {
      headers: { Authorization: `Bearer ${accessToken}` },
    });
    assert.strictEqual(tasksRes.status, 200, "GET /external/v1/tasksが200でない");
    const body = await tasksRes.json();
    assert.ok(Array.isArray(body.tasks), "tasksフィールドが配列でない");
    // 既定値(backend.external-tasks-pagination-v2=false、offsetページング形状)
    assert.strictEqual(body.page, 1);
    assert.strictEqual(body.page_size, 10);
  });
});
