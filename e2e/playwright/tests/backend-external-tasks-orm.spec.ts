import { test, expect } from "@playwright/test";
import { restoreFeatureFlagViaAdmin } from "./helpers";

// CONTRACT.mdセクション24(外部公開APIのTask一覧取得をGORM/bobのどちらで動かすか
// 切り替えるFeature Flag `backend.external-tasks-orm`)の「bobに切り替えても、GORMと
// 完全に同一のレスポンス形状で応答する」ことを検証する。これまでランブックの手順に
// 沿ったcurl+目視でのみ確認されており、自動テストが1つも無かった(e2e/以下で
// external-tasks-orm/external_tasks_ormを検索してもヒット無し、
// backend-task-language.spec.tsの直後に見つかった同種のギャップ)
//
// backend.task-languageと異なり、backend.external-tasks-ormはbackend(Go、コア構成として
// 既定で起動している)自身が評価する軸で、bff・frontendを一切経由しない。そのため
// このテストはブラウザ操作(page)を一切使わず、PlaywrightのAPIRequestContext
// (admin.spec.tsの「無認証確認」テストで既に使われているのと同じ`request`フィクスチャ)
// だけで完結する純粋なHTTPレベルのテストにしている
//
// task-create-ux.spec.ts・backend-task-language.spec.tsと同じ規約: このプロジェクトの
// JS版e2eはmysql2等のMySQLクライアント依存を持たず、feature flagの切り替えは
// 事前にMySQL側/admin画面で行ってから実行する運用にしている
// (admin/goの`POST /flags/:id`は数値の内部id指定が必要で、flag_key指定のAPIが
// 無いため、JS側から自己完結でid解決までするのは複雑さに見合わないと判断した)
//
//   事前に backend.external-tasks-orm を "bob" に切り替えてから:
//   npx playwright test backend-external-tasks-orm.spec.ts
//
//   確認後は backend.external-tasks-orm を "gorm"(既定値)へ戻すこと
//   docker compose exec mysql mysql -uroot bff_gin_development -e \
//     "UPDATE feature_flags SET default_variation='bob' WHERE flag_key='backend.external-tasks-orm';"

const KEYCLOAK_TOKEN_URL =
  process.env.KEYCLOAK_TOKEN_URL ?? "http://localhost:8082/realms/training/protocol/openid-connect/token";
const EXTERNAL_API_CLIENT_ID = process.env.EXTERNAL_API_CLIENT_ID ?? "external-api-client";
const EXTERNAL_API_CLIENT_SECRET =
  process.env.EXTERNAL_API_CLIENT_SECRET ?? "external-api-client-local-dev-secret";
// gateway(:8081)は「追加構成」で既定では起動していないため、gateway未起動でも
// 確認できるようbackend自身の外部公開APIアドレス(:8097)へ直接向ける
// (README.mdに明記された確認方法と同じ)
const EXTERNAL_TASKS_BASE_URL = process.env.BACKEND_EXTERNAL_API_BASE_URL ?? "http://localhost:8097";

test.describe("backend(Go)内でのORM比較: backend.external-tasks-orm=bob", () => {
  // 【e2eのfeature flagクリーンアップ信頼性監査で追加】backend-task-language.spec.tsと
  // 同じ理由(assertion失敗時でも既定値へ確実に戻すため)
  test.afterEach(async ({ request }) => {
    await restoreFeatureFlagViaAdmin(request, "backend.external-tasks-orm", "gorm");
  });

  test("GET /external/v1/tasks が、GORMのときと完全に同じレスポンス形状(offsetページング)で200を返す", async ({
    request,
  }) => {
    const tokenRes = await request.post(KEYCLOAK_TOKEN_URL, {
      form: {
        grant_type: "client_credentials",
        client_id: EXTERNAL_API_CLIENT_ID,
        client_secret: EXTERNAL_API_CLIENT_SECRET,
      },
    });
    expect(tokenRes.ok(), "Keycloakのトークン取得に失敗した").toBeTruthy();
    const { access_token: accessToken } = await tokenRes.json();
    expect(accessToken).toBeTruthy();

    const tasksRes = await request.get(`${EXTERNAL_TASKS_BASE_URL}/external/v1/tasks?user_id=1`, {
      headers: { Authorization: `Bearer ${accessToken}` },
    });
    expect(tasksRes.status(), await tasksRes.text()).toBe(200);

    const body = await tasksRes.json();
    // レスポンス形状はGORM実装と完全に同一のはず(CONTRACT.mdセクション24、
    // backend.external-tasks-pagination-v2=falseの既定ではoffsetページング形状)
    expect(Array.isArray(body.tasks)).toBe(true);
    expect(body.page).toBe(1);
    expect(body.page_size).toBe(10);
    expect(typeof body.total).toBe("number");
  });
});
