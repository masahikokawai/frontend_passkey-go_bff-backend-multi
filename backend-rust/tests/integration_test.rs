//! DB(docker-compose上のMySQL)に実接続する結合テスト
//! backend(Go)の backend/test/integration が `-tags=integration` で通常のテストから
//! 分離されているのと同じ考え方で、既定の`cargo test`では実行されない`#[ignore]`にしてある
//! 実行するには: docker compose up -d --wait mysql してから
//!   cargo test -- --ignored
//!
//! テストごとに一意なメール/keycloak_subでユーザー行を作り、終了時に必ず削除する
//! (共有の開発用DBを汚さないため)

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use axum::body::{to_bytes, Body};
use axum::http::{Request, StatusCode};
use backend_rust::auth::jwt::{is_local_issuer, Claims, Dispatcher, HmacVerifier, LOCAL_HMAC_ISSUER};
use backend_rust::auth::{resolve_user_id, ResolveOutcome};
use backend_rust::{db, rest, AppState};
use serde_json::{json, Value};
use tower::ServiceExt;

const TEST_HMAC_SECRET: &str = "integration-test-hmac-secret";
const TEST_AUDIENCE: &str = "backend";

fn test_dsn() -> String {
    std::env::var("DB_DSN_URL").unwrap_or_else(|_| "mysql://root@127.0.0.1:13306/bff_gin_development".to_string())
}

static COUNTER: AtomicU64 = AtomicU64::new(0);

fn unique_suffix() -> String {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos();
    let n = COUNTER.fetch_add(1, Ordering::SeqCst);
    format!("{}-{}", nanos, n)
}

/// テスト用ユーザーを1行作り、そのidを返す
/// 呼び出し側は必ずcleanup_userで消すこと
/// 【backend(Go)との突き合わせで判明】keycloak_subはusersテーブルではなく、
/// 別テーブルuser_keycloaksに持つ(migration 000008_split_user_credentials)ため、
/// 2テーブルへ分けて挿入する
async fn create_test_user(pool: &sqlx::MySqlPool, keycloak_sub: &str, email: &str) -> u64 {
    let result = sqlx::query(
        "INSERT INTO users (email, name, role, created_at, updated_at) VALUES (?, 'Integration Test User', 1, NOW(), NOW())",
    )
    .bind(email)
    .execute(pool)
    .await
    .expect("テストユーザー作成に失敗");
    let user_id = result.last_insert_id();

    sqlx::query(
        "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) VALUES (?, ?, NOW(), NOW())",
    )
    .bind(user_id)
    .bind(keycloak_sub)
    .execute(pool)
    .await
    .expect("テスト用user_keycloaks作成に失敗");

    user_id
}

async fn cleanup_user(pool: &sqlx::MySqlPool, user_id: u64) {
    let _ = sqlx::query("DELETE FROM tasks WHERE user_id = ?").bind(user_id).execute(pool).await;
    let _ = sqlx::query("DELETE FROM user_keycloaks WHERE user_id = ?").bind(user_id).execute(pool).await;
    let _ = sqlx::query("DELETE FROM users WHERE id = ?").bind(user_id).execute(pool).await;
}

fn test_dispatcher() -> Dispatcher {
    Dispatcher::new().register(
        LOCAL_HMAC_ISSUER,
        Box::new(HmacVerifier::new(TEST_HMAC_SECRET, LOCAL_HMAC_ISSUER, TEST_AUDIENCE)),
    )
}

fn make_hmac_token(sub: &str) -> String {
    use jsonwebtoken::{encode, EncodingKey, Header};
    use serde::Serialize;
    #[derive(Serialize)]
    struct RawClaims<'a> {
        sub: &'a str,
        iss: &'a str,
        aud: &'a str,
        exp: i64,
    }
    let claims = RawClaims { sub, iss: LOCAL_HMAC_ISSUER, aud: TEST_AUDIENCE, exp: chrono::Utc::now().timestamp() + 3600 };
    encode(&Header::new(jsonwebtoken::Algorithm::HS256), &claims, &EncodingKey::from_secret(TEST_HMAC_SECRET.as_bytes())).unwrap()
}

// --- resolve_user_id ---

#[tokio::test]
#[ignore]
async fn resolve_user_id_local_issuer_known_user_resolves() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした(docker compose up -d --wait mysqlが必要)");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("integration-kc-{}", suffix), &format!("integration-{}@example.com", suffix)).await;

    let claims = Claims {
        sub: user_id.to_string(),
        iss: LOCAL_HMAC_ISSUER.to_string(),
        exp: 0,
        nbf: None,
        aud: None,
        preferred_username: String::new(),
        email: String::new(),
        name: String::new(),
        realm_access: Default::default(),
        azp: String::new(),
    };

    let outcome = resolve_user_id(&pool, &claims).await;
    cleanup_user(&pool, user_id).await;

    match outcome {
        ResolveOutcome::Ok(id) => assert_eq!(id, user_id),
        ResolveOutcome::UserNotProvisioned => panic!("既存ユーザーのはずがuser_not_provisionedになった"),
    }
}

#[tokio::test]
#[ignore]
async fn resolve_user_id_local_issuer_unknown_user_is_not_provisioned() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let claims = Claims {
        sub: "999999999".to_string(), // 存在しないid
        iss: LOCAL_HMAC_ISSUER.to_string(),
        exp: 0,
        nbf: None,
        aud: None,
        preferred_username: String::new(),
        email: String::new(),
        name: String::new(),
        realm_access: Default::default(),
        azp: String::new(),
    };
    let outcome = resolve_user_id(&pool, &claims).await;
    assert!(matches!(outcome, ResolveOutcome::UserNotProvisioned));
}

#[tokio::test]
#[ignore]
async fn resolve_user_id_keycloak_issuer_looks_up_by_keycloak_sub() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let suffix = unique_suffix();
    let sub = format!("integration-kc-sub-{}", suffix);
    let user_id = create_test_user(&pool, &sub, &format!("integration-kc-{}@example.com", suffix)).await;

    let claims = Claims {
        sub: sub.clone(),
        iss: "http://localhost:8082/realms/training".to_string(), // ローカル発行issuerではない
        exp: 0,
        nbf: None,
        aud: None,
        preferred_username: String::new(),
        email: String::new(),
        name: String::new(),
        realm_access: Default::default(),
        azp: String::new(),
    };
    assert!(!is_local_issuer(&claims.iss));

    let outcome = resolve_user_id(&pool, &claims).await;
    cleanup_user(&pool, user_id).await;

    match outcome {
        ResolveOutcome::Ok(id) => assert_eq!(id, user_id),
        ResolveOutcome::UserNotProvisioned => panic!("keycloak_subが一致するユーザーのはずがuser_not_provisionedになった"),
    }
}

#[tokio::test]
#[ignore]
async fn resolve_user_id_keycloak_issuer_unknown_sub_is_not_provisioned() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let claims = Claims {
        sub: format!("nonexistent-sub-{}", unique_suffix()),
        iss: "http://localhost:8082/realms/training".to_string(),
        exp: 0,
        nbf: None,
        aud: None,
        preferred_username: String::new(),
        email: String::new(),
        name: String::new(),
        realm_access: Default::default(),
        azp: String::new(),
    };
    let outcome = resolve_user_id(&pool, &claims).await;
    assert!(matches!(outcome, ResolveOutcome::UserNotProvisioned));
}

/// 【セキュリティ監査で追加】
/// backend の RequireAuth相当(このRust実装ではresolve_user_id呼び出し側)は JWT の azp(authorized party)クレームを一切見ていない
///
/// そのため、外部公開API用の
/// Client Credentials Grantトークン(external-api-clientが取得する、aud=backendが注入された
/// トークン)は、署名検証・iss/aud/exp等の「認証」自体は内部APIも通過してしまう
///
/// これが安全なのは、external-api-client自身のサービスアカウントsub
/// (Keycloakの慣例で"service-account-external-api-client"のような形式になる)が、
/// JITプロビジョニング(通常ユーザーのログイン時のみ実行される)を一度も経ておらず、
/// usersテーブルに該当行が存在しないため、resolve_user_idが必ずUserNotProvisionedを
/// 返すという「2段構えの安全性」に依存しているからである
///
/// このテストは、その2段構えの安全性が実際に効いていることを明示的に固定し、
/// 将来resolve_user_idの実装が変わってこの前提が崩れた場合に検知できるようにする
/// (Go実装backend/で同じ観点のテストが既に存在し、4言語比較実装すべてに同じ検証を追加する一環)
/// azpクレームの値そのものは意図的に無視されるべき設計のため、
/// このテストでもazp="external-api-client"を設定しつつ、それが判定に一切影響しない
/// (subだけで拒否される)ことを確認する
#[tokio::test]
#[ignore]
async fn resolve_user_id_external_api_client_service_account_token_cannot_reach_internal_api() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    // Keycloakのservice account subの実際の命名慣例を模したsub
    // このsubはJITプロビジョニングを経ないため、usersテーブルには絶対に存在しない
    let claims = Claims {
        sub: "service-account-external-api-client".to_string(),
        iss: "http://localhost:8082/realms/training".to_string(),
        exp: 0,
        nbf: None,
        aud: Some(serde_json::json!(["backend"])),
        preferred_username: String::new(),
        email: String::new(),
        name: String::new(),
        realm_access: Default::default(),
        azp: "external-api-client".to_string(),
    };
    assert!(!is_local_issuer(&claims.iss));

    let outcome = resolve_user_id(&pool, &claims).await;

    assert!(
        matches!(outcome, ResolveOutcome::UserNotProvisioned),
        "external-api-clientのサービスアカウントトークンで内部APIのuser解決が成功してしまった(2段構えの安全性が崩れている)"
    );
}

// --- REST v1のCRUD + ステータスコードマッピング(実DB) ---

async fn json_body(resp: axum::response::Response) -> Value {
    let bytes = to_bytes(resp.into_body(), usize::MAX).await.unwrap();
    if bytes.is_empty() {
        Value::Null
    } else {
        serde_json::from_slice(&bytes).unwrap()
    }
}

/// アサーション失敗(panic)が起きても必ずcleanup_userが走るよう、本体を
/// tokio::spawnした別タスクとして実行しJoinErrorで受け止める(共有の開発用DBにテストの残骸を残さないため
/// catch_unwindはFutureに直接使えないための代替策)
#[tokio::test]
#[ignore]
async fn rest_task_crud_round_trip_against_real_db() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("integration-crud-{}", suffix), &format!("integration-crud-{}@example.com", suffix)).await;

    let body_pool = pool.clone();
    let result = tokio::spawn(async move { rest_task_crud_body(body_pool, user_id).await }).await;

    cleanup_user(&pool, user_id).await;
    if let Err(e) = result {
        if e.is_panic() {
            std::panic::resume_unwind(e.into_panic());
        }
    }
}

async fn rest_task_crud_body(pool: sqlx::MySqlPool, user_id: u64) {
    let state = Arc::new(AppState { pool: pool.clone(), dispatcher: test_dispatcher() });
    let token = make_hmac_token(&user_id.to_string());
    let auth_header = format!("Bearer {}", token);

    let app = || rest::router(state.clone());

    // Create
    let create_body = json!({
        "name": "it-crud-task",
        "status": "waiting",
        "finished_on": "2030-01-01",
        "label_ids": []
    });
    let resp = app()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/internal/v1/tasks")
                .header("content-type", "application/json")
                .header("authorization", &auth_header)
                .body(Body::from(create_body.to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    let status = resp.status();
    let created = json_body(resp).await;
    assert_eq!(status, StatusCode::CREATED, "create should return 201, body={:?}", created);
    let task_id = created["id"].as_u64().expect("created task should have an id");
    assert_eq!(created["name"], json!("it-crud-task"));
    assert_eq!(created["status"], json!("waiting"));
    assert!(created.get("user_id").is_none(), "backend(Go)と同じくuser_idはレスポンスに含めない");

    // List
    let resp = app()
        .oneshot(
            Request::builder()
                .uri("/internal/v1/tasks")
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let list = json_body(resp).await;
    assert!(list["tasks"].as_array().unwrap().iter().any(|t| t["id"] == json!(task_id)));

    // Get
    let resp = app()
        .oneshot(
            Request::builder()
                .uri(format!("/internal/v1/tasks/{}", task_id))
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);

    // Update
    let update_body = json!({"name": "it-updated", "status": "completed", "finished_on": "2030-02-02", "label_ids": []});
    let resp = app()
        .oneshot(
            Request::builder()
                .method("PATCH")
                .uri(format!("/internal/v1/tasks/{}", task_id))
                .header("content-type", "application/json")
                .header("authorization", &auth_header)
                .body(Body::from(update_body.to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let updated = json_body(resp).await;
    assert_eq!(updated["name"], json!("it-updated"));
    assert_eq!(updated["status"], json!("completed"));

    // Delete
    let resp = app()
        .oneshot(
            Request::builder()
                .method("DELETE")
                .uri(format!("/internal/v1/tasks/{}", task_id))
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::NO_CONTENT);

    // 【6回目のテスト監査(DELETE冪等性のクロス言語パリティ角度)で追加】
    // 既に削除済みのidへ再度DELETEを投げても、クラッシュ(500)せず一貫して404
    // (not_found)になることを確認する。rows_affected()==0を「見つからない」として
    // 扱う実装(backend-rust/src/db.rs delete_task)がここで正しく機能していることの回帰確認
    let resp = app()
        .oneshot(
            Request::builder()
                .method("DELETE")
                .uri(format!("/internal/v1/tasks/{}", task_id))
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::NOT_FOUND);
    let body = json_body(resp).await;
    assert_eq!(body, json!({"error": "not_found"}));

    // Get after delete -> 404 not_found
    let resp = app()
        .oneshot(
            Request::builder()
                .uri(format!("/internal/v1/tasks/{}", task_id))
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::NOT_FOUND);
    let body = json_body(resp).await;
    assert_eq!(body, json!({"error": "not_found"}));
}

#[tokio::test]
#[ignore]
async fn rest_task_status_codes_401_403_400_422() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("integration-codes-{}", suffix), &format!("integration-codes-{}@example.com", suffix)).await;

    let body_pool = pool.clone();
    let result = tokio::spawn(async move { rest_task_status_codes_body(body_pool, user_id).await }).await;

    cleanup_user(&pool, user_id).await;
    if let Err(e) = result {
        if e.is_panic() {
            std::panic::resume_unwind(e.into_panic());
        }
    }
}

async fn rest_task_status_codes_body(pool: sqlx::MySqlPool, user_id: u64) {
    let state = Arc::new(AppState { pool: pool.clone(), dispatcher: test_dispatcher() });
    let app = || rest::router(state.clone());

    // 401: Authorizationヘッダ無し
    let resp = app()
        .oneshot(Request::builder().uri("/internal/v1/tasks").body(Body::empty()).unwrap())
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
    assert_eq!(json_body(resp).await, json!({"error": "unauthorized"}));

    // 403: JWTは有効だが対応するusers行が無い(未プロビジョニング)
    let token = make_hmac_token("999999999");
    let resp = app()
        .oneshot(
            Request::builder()
                .uri("/internal/v1/tasks")
                .header("authorization", format!("Bearer {}", token))
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::FORBIDDEN);
    assert_eq!(json_body(resp).await, json!({"error": "user_not_provisioned"}));

    // ここから先は実在ユーザーが必要(呼び出し側が既に作成済みのuser_idを使う)
    let auth_header = format!("Bearer {}", make_hmac_token(&user_id.to_string()));

    // 400: idがuint64としてパースできない
    let resp = app()
        .oneshot(
            Request::builder()
                .uri("/internal/v1/tasks/not-a-number")
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::BAD_REQUEST);
    assert_eq!(json_body(resp).await, json!({"error": "invalid_id"}));

    // 422: statusクエリが不正(List)
    let resp = app()
        .oneshot(
            Request::builder()
                .uri("/internal/v1/tasks?status=not_a_status")
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert_eq!(json_body(resp).await, json!({"error": "invalid_status"}));

    // 422: Createでfinished_onが過去日(service層のvalidation_error)
    let create_body = json!({"name": "x", "status": "waiting", "finished_on": "2000-01-01", "label_ids": []});
    let resp = app()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/internal/v1/tasks")
                .header("content-type", "application/json")
                .header("authorization", &auth_header)
                .body(Body::from(create_body.to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::UNPROCESSABLE_ENTITY);
    let body = json_body(resp).await;
    assert_eq!(body["error"], json!("validation_error"));
    assert!(body["message"].as_str().unwrap().contains("過去日"));

    // 404: 存在しないtask idへのGet
    let resp = app()
        .oneshot(
            Request::builder()
                .uri("/internal/v1/tasks/999999999")
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::NOT_FOUND);
}

// --- NUL文字・制御文字の実DB往復(このセッションの監査で他4言語(Go/GORM・Go/bob・Rails)は
// 既に確認済みだったが、Rust/Scala(http4s)/Scala(Pekko)は時間の都合で未検証のまま残っていた
// ギャップをここで解消する) ---
//
// sqlxはprepared statementのバイナリプロトコルを使うため、Cの文字列(strlenでNULを
// 終端とみなす)のような途中切り詰めは原理上起きないはずだが、実際にMySQLへ保存・読み出し
// して確認するまでは推測に過ぎない。ここでは名前にNUL(\0)とSOH(\u{1})を埋め込み、
// create→get→listの全経路でバイト列が完全に往復することを実際のDBで確認する
#[tokio::test]
#[ignore]
async fn rest_task_name_with_nul_and_control_bytes_round_trips_against_real_db() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("integration-nul-{}", suffix), &format!("integration-nul-{}@example.com", suffix)).await;

    let body_pool = pool.clone();
    let result = tokio::spawn(async move { rest_task_nul_byte_body(body_pool, user_id).await }).await;

    cleanup_user(&pool, user_id).await;
    if let Err(e) = result {
        if e.is_panic() {
            std::panic::resume_unwind(e.into_panic());
        }
    }
}

async fn rest_task_nul_byte_body(pool: sqlx::MySqlPool, user_id: u64) {
    let state = Arc::new(AppState { pool: pool.clone(), dispatcher: test_dispatcher() });
    let token = make_hmac_token(&user_id.to_string());
    let auth_header = format!("Bearer {}", token);
    let app = || rest::router(state.clone());

    // "evil\0\u{1}name" (末尾の"name"が生きていること=NULで切り詰められていないことの証拠)
    let nul_name = "evil\0\u{1}name";
    let create_body = json!({
        "name": nul_name,
        "description": "desc\0with\u{1}control\u{1f}chars",
        "status": "waiting",
        "finished_on": "2030-01-01",
        "label_ids": []
    });
    let resp = app()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/internal/v1/tasks")
                .header("content-type", "application/json")
                .header("authorization", &auth_header)
                .body(Body::from(create_body.to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    let status = resp.status();
    let created = json_body(resp).await;
    assert_eq!(status, StatusCode::CREATED, "create should return 201, body={:?}", created);
    let task_id = created["id"].as_u64().expect("created task should have an id");
    // 作成レスポンス自体で既にNUL文字が生き残っている(=シリアライズ層で切り詰められていない)ことを確認
    assert_eq!(
        created["name"].as_str().unwrap(),
        nul_name,
        "作成直後のレスポンスでNUL/制御文字入りの名前が切り詰め・破損している"
    );

    // Get: DBへ実際に保存→読み出しした経路で、バイト列が完全に往復することを確認
    let resp = app()
        .oneshot(
            Request::builder()
                .uri(format!("/internal/v1/tasks/{}", task_id))
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let fetched = json_body(resp).await;
    assert_eq!(
        fetched["name"].as_str().unwrap(),
        nul_name,
        "DBへの保存・読み出しを経由するとNUL/制御文字入りの名前が切り詰め・破損している(sqlx/MySQLの往復に問題がある)"
    );
    assert_eq!(
        fetched["description"].as_str().unwrap(),
        "desc\0with\u{1}control\u{1f}chars",
        "descriptionフィールドでも同様にNUL/制御文字が保持されていること"
    );

    // List経由でも同じ値が壊れずに返ってくることを確認(一覧取得は別のクエリ経路のため念のため)
    let resp = app()
        .oneshot(
            Request::builder()
                .uri("/internal/v1/tasks")
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    assert_eq!(resp.status(), StatusCode::OK);
    let list = json_body(resp).await;
    let found = list["tasks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|t| t["id"] == json!(task_id))
        .expect("作成したタスクが一覧に見つからない");
    assert_eq!(found["name"].as_str().unwrap(), nul_name);
}

// 【テスト監査(9回目、重複label_idsという角度)で発見・修正した実バグ】
// 同じlabel_idを複数回渡すと(例: [id, id])、`replace_labels`が重複除去せず
// そのままループでINSERTしていたため、2回目のINSERTでtask_labelsの
// (task_id,label_id)へのUNIQUE制約(migrations/000004)に違反し、生のMySQLエラー
// (1062 Duplicate entry)がそのまま伝播していた(Go/Scala(http4s)実装で見つかった
// 同種のバグと同じ根本原因)。src/db.rsのreplace_labelsに重複除去を追加して修正
#[tokio::test]
#[ignore]
async fn create_task_with_duplicate_label_id_dedupes_cleanly() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let suffix = unique_suffix();
    let user_id =
        create_test_user(&pool, &format!("integration-duplabel-{}", suffix), &format!("integration-duplabel-{}@example.com", suffix)).await;

    let label_id: u64 = sqlx::query_scalar("SELECT id FROM labels ORDER BY id LIMIT 1")
        .fetch_one(&pool)
        .await
        .expect("labelsシード(migrations/000005)が前提");

    let state = Arc::new(AppState { pool: pool.clone(), dispatcher: test_dispatcher() });
    let token = make_hmac_token(&user_id.to_string());
    let auth_header = format!("Bearer {}", token);

    let create_body = json!({
        "name": "dup-label-test",
        "status": "waiting",
        "finished_on": "2030-01-01",
        "label_ids": [label_id, label_id]
    });
    let resp = rest::router(state.clone())
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/internal/v1/tasks")
                .header("content-type", "application/json")
                .header("authorization", &auth_header)
                .body(Body::from(create_body.to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    let status = resp.status();
    let created = json_body(resp).await;
    assert_eq!(status, StatusCode::CREATED, "重複label_idを含むcreateが失敗した: {:?}", created);
    let task_id = created["id"].as_u64().unwrap();

    let count: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM task_labels WHERE task_id = ? AND label_id = ?")
        .bind(task_id)
        .bind(label_id)
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(count, 1, "重複除去されず複数行が作られている");

    sqlx::query("DELETE FROM tasks WHERE id = ?").bind(task_id).execute(&pool).await.unwrap();
    cleanup_user(&pool, user_id).await;
}

// 【回帰確認・既知バグ・未修正】backend-js-express/backend-js-ts-express実装時の実機検証で発覚:
// src/db.rs の delete_task は `DELETE FROM tasks WHERE id = ? AND user_id = ?` のみを実行し、
// task_labels を削除しない。task_labels(migrations/000004)には外部キー制約が無いため、
// タスク削除後もラベル関連付けの行がDBに孤立して残り続けるが、DELETEのHTTPレスポンス自体は
// 204で成功して見えるため、API経由のブラックボックステストだけでは気付けない
// backend(Go)・backend-js-express・backend-js-ts-expressは同じ削除操作で task_labels → tasks の順に削除する
// よう実装済みで、この孤立行問題は起きない
// このテストは現状のRust実装に対しては意図的に失敗する(orphaned_countが0にならない)
// src/db.rs の delete_task で task_labels を先に削除するよう修正すれば通るようになる
#[tokio::test]
#[ignore]
async fn delete_task_removes_task_labels_rows_known_bug_in_rust() {
    let pool = db::connect(&test_dsn()).await.expect("MySQLへ接続できませんでした");
    let suffix = unique_suffix();
    let user_id = create_test_user(
        &pool,
        &format!("integration-orphanlabel-{}", suffix),
        &format!("integration-orphanlabel-{}@example.com", suffix),
    )
    .await;

    // 既存の共有labels行(create_task_with_duplicate_label_id_dedupes_cleanlyテスト等も
    // 使う「先頭のlabel」)を使い回すと、並列実行時に同じlabel_idへのtask_labels挿入が
    // 競合し、他テスト側で意図しない500(内部エラー)を誘発することが実機検証で判明した
    // このテスト専用の一意な名前のlabelを作成し、他テストと競合しないようにする
    let label_name = format!("integration-orphanlabel-{}", suffix);
    let label_insert = sqlx::query("INSERT INTO labels (name, created_at, updated_at) VALUES (?, NOW(), NOW())")
        .bind(&label_name)
        .execute(&pool)
        .await
        .expect("テスト専用labelの作成に失敗");
    let label_id: u64 = label_insert.last_insert_id();

    let state = Arc::new(AppState { pool: pool.clone(), dispatcher: test_dispatcher() });
    let token = make_hmac_token(&user_id.to_string());
    let auth_header = format!("Bearer {}", token);
    let app = || rest::router(state.clone());

    // ラベル付きタスクを作成
    let create_body = json!({
        "name": "orphan-label-test",
        "status": "waiting",
        "finished_on": "2030-01-01",
        "label_ids": [label_id]
    });
    let resp = app()
        .oneshot(
            Request::builder()
                .method("POST")
                .uri("/internal/v1/tasks")
                .header("content-type", "application/json")
                .header("authorization", &auth_header)
                .body(Body::from(create_body.to_string()))
                .unwrap(),
        )
        .await
        .unwrap();
    let created = json_body(resp).await;
    let task_id = created["id"].as_u64().expect("作成したタスクにidが無い");

    // 削除
    let resp = app()
        .oneshot(
            Request::builder()
                .method("DELETE")
                .uri(format!("/internal/v1/tasks/{}", task_id))
                .header("authorization", &auth_header)
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();
    let delete_status = resp.status();

    // 孤立行の件数を先に取得しておく(assertより前に評価し、下の後片付けを必ず実行させるため)
    let orphaned_count: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM task_labels WHERE task_id = ?")
        .bind(task_id)
        .fetch_one(&pool)
        .await
        .unwrap();

    // 後片付け: task_labelsに外部キー制約が無くtasks側の削除だけでは消えないため明示的に削除する
    // (このテストが失敗する場合でも、共有の開発用DBに孤立行を残さないようにする)
    // cleanup_user と同様に、後片付けは最善努力(エラーを無視)で行う
    // (並列実行時のDB混雑等で一時的に失敗しても、後続の削除が必ず実行されるようにするため)
    let _ = sqlx::query("DELETE FROM task_labels WHERE task_id = ?").bind(task_id).execute(&pool).await;
    let _ = sqlx::query("DELETE FROM labels WHERE id = ?").bind(label_id).execute(&pool).await;
    cleanup_user(&pool, user_id).await;

    assert_eq!(delete_status, StatusCode::NO_CONTENT, "タスク削除自体のHTTPレスポンスは成功するはず");
    assert_eq!(
        orphaned_count,
        0,
        "task_labelsに孤立行が残っている(既知バグ: backend-rustのdelete_taskはtask_labelsを削除しない。backend(Go)/backend-js-express/backend-js-ts-expressは修正済み)"
    );
}
