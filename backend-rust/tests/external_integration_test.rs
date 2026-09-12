//! 外部公開API(CONTRACT.mdセクション11)のうち、実DBに接続しないと確認できない部分の結合テスト。
//! `integration_test.rs`と同じ考え方(既定の`cargo test`では実行されない`#[ignore]`)。
//! 実行するには: docker compose up -d --wait mysql してから
//!   cargo test -- --ignored
//!
//! 認証(Keycloak JWKS経由のトークン検証)は実際のKeycloakへのネットワークアクセスが要るため、
//! ここでは扱わない(`external::authenticate`のazp比較ロジック自体は単体テストで、
//! JWKS検証込みの実際の疎通は実機のcurl確認で担保している。README参照)。

use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use backend_rust::{db, flags};
use chrono::{NaiveDate, Utc};

fn test_dsn() -> String {
    std::env::var("DB_DSN_URL").unwrap_or_else(|_| "mysql://root@127.0.0.1:13306/bff_gin_development".to_string())
}

static COUNTER: AtomicU64 = AtomicU64::new(0);

fn unique_suffix() -> String {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos();
    let n = COUNTER.fetch_add(1, Ordering::SeqCst);
    format!("{}-{}", nanos, n)
}

async fn create_test_user(pool: &sqlx::MySqlPool, email: &str) -> u64 {
    let result = sqlx::query(
        "INSERT INTO users (email, name, role, created_at, updated_at) VALUES (?, 'External API Test User', 1, NOW(), NOW())",
    )
    .bind(email)
    .execute(pool)
    .await
    .expect("テストユーザー作成に失敗");
    result.last_insert_id()
}

async fn cleanup_user(pool: &sqlx::MySqlPool, user_id: u64) {
    let _ = sqlx::query("DELETE FROM tasks WHERE user_id = ?").bind(user_id).execute(pool).await;
    let _ = sqlx::query("DELETE FROM users WHERE id = ?").bind(user_id).execute(pool).await;
}

/// tasks.nameはVARCHAR(20)のため、呼び出し側が長い一意サフィックスを渡しても
/// 常に20文字以内に収める(1406 Data too long対策)
async fn create_task_for(pool: &sqlx::MySqlPool, user_id: u64, name: &str) -> u64 {
    let name: String = name.chars().take(20).collect();
    let now = Utc::now().naive_utc();
    let result = sqlx::query(
        "INSERT INTO tasks (name, status, finished_on, user_id, created_at, updated_at) VALUES (?, 1, ?, ?, ?, ?)",
    )
    .bind(&name)
    .bind(NaiveDate::from_ymd_opt(2030, 1, 1).unwrap())
    .bind(user_id)
    .bind(now)
    .bind(now)
    .execute(pool)
    .await
    .expect("テストタスク作成に失敗");
    result.last_insert_id()
}

/// backend.external-tasks-pagination-v2 は5言語で共有する1つのflag(CONTRACT.mdセクション20)。
/// enabled/default_variation/variationsを直接書き換えてfetch_boolが正しく追従することを確認する
#[tokio::test]
#[ignore]
async fn fetch_bool_reflects_enabled_and_default_variation() {
    let pool = sqlx::MySqlPool::connect(&test_dsn()).await.expect("DB接続に失敗");

    // 現状の値を退避し、テスト後に必ず復元する
    let before: (i8, String) = sqlx::query_as(
        "SELECT enabled, default_variation FROM feature_flags WHERE flag_key = 'backend.external-tasks-pagination-v2'",
    )
    .fetch_one(&pool)
    .await
    .expect("既存のflag行取得に失敗");

    sqlx::query("UPDATE feature_flags SET enabled = 1, default_variation = 'on' WHERE flag_key = 'backend.external-tasks-pagination-v2'")
        .execute(&pool)
        .await
        .expect("flag更新(on)に失敗");
    let on = flags::fetch_bool(&pool, "backend.external-tasks-pagination-v2").await;
    assert_eq!(on, Some(true));

    sqlx::query("UPDATE feature_flags SET enabled = 1, default_variation = 'off' WHERE flag_key = 'backend.external-tasks-pagination-v2'")
        .execute(&pool)
        .await
        .expect("flag更新(off)に失敗");
    let off = flags::fetch_bool(&pool, "backend.external-tasks-pagination-v2").await;
    assert_eq!(off, Some(false));

    sqlx::query("UPDATE feature_flags SET enabled = 0, default_variation = 'on' WHERE flag_key = 'backend.external-tasks-pagination-v2'")
        .execute(&pool)
        .await
        .expect("flag更新(disabled)に失敗");
    let disabled = flags::fetch_bool(&pool, "backend.external-tasks-pagination-v2").await;
    assert_eq!(disabled, Some(false), "enabled=falseならdefault_variationに関わらずfalse");

    // 復元
    sqlx::query("UPDATE feature_flags SET enabled = ?, default_variation = ? WHERE flag_key = 'backend.external-tasks-pagination-v2'")
        .bind(before.0)
        .bind(before.1)
        .execute(&pool)
        .await
        .expect("flag復元に失敗");
}

/// backend(Go)のListOffsetForExternalAPIと同じソート順(created_at DESC, id DESC)・total件数を確認する
#[tokio::test]
#[ignore]
async fn list_tasks_offset_external_orders_by_created_at_desc_then_id_desc() {
    let pool = sqlx::MySqlPool::connect(&test_dsn()).await.expect("DB接続に失敗");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("ext-offset-{}@example.com", suffix)).await;

    let id1 = create_task_for(&pool, user_id, &format!("task-a-{}", suffix)).await;
    let id2 = create_task_for(&pool, user_id, &format!("task-b-{}", suffix)).await;
    let id3 = create_task_for(&pool, user_id, &format!("task-c-{}", suffix)).await;

    let (tasks, total) = db::list_tasks_offset_external(&pool, user_id, 1, 10)
        .await
        .expect("list_tasks_offset_external should succeed");

    assert_eq!(total, 3);
    // 同一created_atになりうる(同一トランザクション内で高速に作られるため)ので、
    // tie-breakのid DESCにより新しいid(id3)が先頭に来ることを確認する
    assert_eq!(tasks[0].id, id3);
    let ids: Vec<u64> = tasks.iter().map(|t| t.id).collect();
    assert!(ids.contains(&id1) && ids.contains(&id2) && ids.contains(&id3));

    cleanup_user(&pool, user_id).await;
}

/// backend(Go)のListCursorForExternalAPIと同じkeyset絞り込み条件を確認する:
/// 1ページ目の最後のcursorを使うと、2ページ目には重複無く続きが返る
#[tokio::test]
#[ignore]
async fn list_tasks_cursor_external_pages_without_duplicates() {
    let pool = sqlx::MySqlPool::connect(&test_dsn()).await.expect("DB接続に失敗");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("ext-cursor-{}@example.com", suffix)).await;

    let mut created_ids = Vec::new();
    for i in 0..5 {
        created_ids.push(create_task_for(&pool, user_id, &format!("task-{}-{}", i, suffix)).await);
    }

    let page1 = db::list_tasks_cursor_external(&pool, user_id, None, 2)
        .await
        .expect("1ページ目取得に失敗");
    assert_eq!(page1.len(), 2);

    let last = page1.last().unwrap();
    let after = Some((last.created_at, last.id));
    let page2 = db::list_tasks_cursor_external(&pool, user_id, after, 2)
        .await
        .expect("2ページ目取得に失敗");
    assert_eq!(page2.len(), 2);

    let page1_ids: Vec<u64> = page1.iter().map(|t| t.id).collect();
    let page2_ids: Vec<u64> = page2.iter().map(|t| t.id).collect();
    for id in &page2_ids {
        assert!(!page1_ids.contains(id), "ページ間で重複しないこと");
    }

    cleanup_user(&pool, user_id).await;
}

// 【テストカバレッジ監査で追記】0件・ちょうどlimit件、という境界値は、Scala(Pekko)実装には
// 既にテストがあった(ExternalTaskRoutesSpec)が、Rust実装には無かった非対称性を埋める。
// 4言語とも同じワイヤー契約を実装しているはずなので、境界値のテスト観点も揃えるべき。

/// タスクが0件のユーザーに対するoffset一覧は、空配列・total=0を返す(エラーにはならない)
#[tokio::test]
#[ignore]
async fn list_tasks_offset_external_returns_empty_for_user_with_no_tasks() {
    let pool = sqlx::MySqlPool::connect(&test_dsn()).await.expect("DB接続に失敗");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("ext-offset-empty-{}@example.com", suffix)).await;

    let (tasks, total) = db::list_tasks_offset_external(&pool, user_id, 1, 10)
        .await
        .expect("list_tasks_offset_external should succeed even with zero tasks");

    assert!(tasks.is_empty());
    assert_eq!(total, 0);

    cleanup_user(&pool, user_id).await;
}

/// タスクが0件のユーザーに対するcursor一覧は空配列を返す
/// (呼び出し側(handler)がlen==limitでnext_cursorを決めるため、ここではDB層が
/// 単に空配列を返すことだけを確認する。next_cursor=nilになる判定はhandler側の責務)
#[tokio::test]
#[ignore]
async fn list_tasks_cursor_external_returns_empty_for_user_with_no_tasks() {
    let pool = sqlx::MySqlPool::connect(&test_dsn()).await.expect("DB接続に失敗");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("ext-cursor-empty-{}@example.com", suffix)).await;

    let tasks = db::list_tasks_cursor_external(&pool, user_id, None, 10)
        .await
        .expect("list_tasks_cursor_external should succeed even with zero tasks");

    assert!(tasks.is_empty());

    cleanup_user(&pool, user_id).await;
}

/// ちょうどlimit件のタスクがある場合、cursor一覧はlimit件を返す
/// (handler層でlen==limitならnext_cursorを発行する契約なので、ここでは
/// 「limitちょうどでもDB層はエラーなく全件返す」ことだけを確認する境界値テスト)
#[tokio::test]
#[ignore]
async fn list_tasks_cursor_external_returns_exactly_limit_when_count_equals_limit() {
    let pool = sqlx::MySqlPool::connect(&test_dsn()).await.expect("DB接続に失敗");
    let suffix = unique_suffix();
    let user_id = create_test_user(&pool, &format!("ext-cursor-exact-{}@example.com", suffix)).await;

    for i in 0..3 {
        create_task_for(&pool, user_id, &format!("task-{}-{}", i, suffix)).await;
    }

    let tasks = db::list_tasks_cursor_external(&pool, user_id, None, 3)
        .await
        .expect("list_tasks_cursor_external should succeed");

    assert_eq!(tasks.len(), 3);

    cleanup_user(&pool, user_id).await;
}
