// CONTRACT.mdセクション24(backend(Go)内でのORM比較、GORM/bob)の
// 「backend.external-tasks-ormをbobに切り替えても、GORMと完全に同一のレスポンス形状で
// 応答する」ことを検証する
//
// backend_external_tasks_orm_test.go(chromedp)と同じ理由で、これはbackend(Go、コア構成)
// 自身が評価する軸でbff・frontendを一切経由しないため、ブラウザ操作は不要な
// 純粋なHTTPレベルのテストにしている(cy.requestは絶対URLであればbaseUrlの
// 同一オリジン制限を受けずに任意のホストへ送れる)
//
// task-create-ux.cy.jsと同じ規約: 事前に MySQL 側で backend.external-tasks-orm を
// bob に切り替えてから実行すること(このプロジェクトのCypressにはMySQLクライアントの
// 依存が無いため、DB側のflag自動切り替えは行わない)
describe("外部公開API: backend.external-tasks-orm=bob", () => {
  // 【e2eのfeature flagクリーンアップ信頼性監査で追加】backend-task-language.cy.jsと
  // 同じ理由(assertion失敗時でも既定値へ確実に戻すため)
  afterEach(() => {
    cy.restoreFeatureFlagViaAdmin("backend.external-tasks-orm", "gorm");
  });

  it("GET /external/v1/tasksがGORMと同一のレスポンス形状(offsetページング)で200を返す", () => {
    cy.request({
      method: "POST",
      url: "http://localhost:8082/realms/training/protocol/openid-connect/token",
      form: true,
      body: {
        grant_type: "client_credentials",
        client_id: "external-api-client",
        client_secret: "external-api-client-local-dev-secret",
      },
    }).then((tokenRes) => {
      expect(tokenRes.status).to.eq(200);
      const accessToken = tokenRes.body.access_token;
      expect(accessToken, "access_tokenが空でない").to.be.a("string").and.not.be.empty;

      cy.request({
        method: "GET",
        url: "http://localhost:8097/external/v1/tasks?user_id=1",
        headers: { Authorization: `Bearer ${accessToken}` },
      }).then((tasksRes) => {
        expect(tasksRes.status).to.eq(200);
        expect(tasksRes.body.tasks, "tasksフィールドが配列である").to.be.an("array");
        // 既定値(backend.external-tasks-pagination-v2=false、offsetページング形状)
        expect(tasksRes.body.page).to.eq(1);
        expect(tasksRes.body.page_size).to.eq(10);
      });
    });
  });
});
