use chrono::{NaiveDate, NaiveDateTime, Utc};
use sqlx::mysql::MySqlPool;
use sqlx::Row;
use std::collections::HashMap;

use crate::model::{Label, Task, TaskInput, TaskStatus, User};

pub async fn connect(dsn: &str) -> Result<MySqlPool, sqlx::Error> {
    MySqlPool::connect(dsn).await
}

// 【backend(Go)実装との突き合わせで判明した点】usersテーブルはkeycloak_subカラムを
// 持たない(migration 000008_split_user_credentials.up.sqlでuser_keycloaksテーブルへ分離済み、CONTRACT.mdセクション16.2)
// backend(Go)のrepository.User.GetByKeycloakSubもuser_keycloaksをJOINしているため、それに合わせている
pub async fn find_user_by_id(pool: &MySqlPool, id: u64) -> Option<User> {
    sqlx::query("SELECT id, email, name FROM users WHERE id = ?")
        .bind(id)
        .fetch_optional(pool)
        .await
        .ok()
        .flatten()
        .map(row_to_user)
}

pub async fn find_user_by_keycloak_sub(pool: &MySqlPool, sub: &str) -> Option<User> {
    sqlx::query(
        "SELECT users.id AS id, users.email AS email, users.name AS name \
         FROM users JOIN user_keycloaks ON user_keycloaks.user_id = users.id \
         WHERE user_keycloaks.keycloak_sub = ?",
    )
    .bind(sub)
    .fetch_optional(pool)
    .await
    .ok()
    .flatten()
    .map(row_to_user)
}

fn row_to_user(row: sqlx::mysql::MySqlRow) -> User {
    User {
        id: row.get::<u64, _>("id"),
        email: row.get("email"),
        name: row.get("name"),
    }
}

/// name/status/label_idsの絞り込み条件をWHERE句へ組み立てる
/// (backend(Go)の repository.Task.scoped と同じ条件: nameはLIKE部分一致、
/// statusは完全一致、label_idsは「いずれかのラベルを持つ」タスク)
struct FilterClauses {
    sql: String,
    binds: Vec<Bind>,
}

enum Bind {
    Str(String),
    U64(u64),
    U8(u8),
}

fn build_filter(user_id: u64, name: &str, status: Option<TaskStatus>, label_ids: &[u64]) -> FilterClauses {
    let mut sql = String::from(" WHERE user_id = ?");
    let mut binds = vec![Bind::U64(user_id)];

    if !name.is_empty() {
        sql.push_str(" AND name LIKE ?");
        binds.push(Bind::Str(format!("%{}%", name)));
    }
    if let Some(st) = status {
        sql.push_str(" AND status = ?");
        binds.push(Bind::U8(st as u8));
    }
    if !label_ids.is_empty() {
        let placeholders = label_ids.iter().map(|_| "?").collect::<Vec<_>>().join(",");
        sql.push_str(&format!(
            " AND id IN (SELECT task_id FROM task_labels WHERE label_id IN ({}))",
            placeholders
        ));
        for id in label_ids {
            binds.push(Bind::U64(*id));
        }
    }

    FilterClauses { sql, binds }
}

fn bind_all<'q>(
    mut q: sqlx::query::Query<'q, sqlx::MySql, sqlx::mysql::MySqlArguments>,
    binds: &'q [Bind],
) -> sqlx::query::Query<'q, sqlx::MySql, sqlx::mysql::MySqlArguments> {
    for b in binds {
        q = match b {
            Bind::Str(s) => q.bind(s.as_str()),
            Bind::U64(v) => q.bind(*v),
            Bind::U8(v) => q.bind(*v),
        };
    }
    q
}

/// v1(REST)向け: offsetページング
/// backend(Go)のListWithoutLabels+applySort相当
/// (N+1の意図的再現はしない。ラベルは一括JOINで取得する。CONTRACT.mdセクション20の
/// 指示により、v1旧実装のN+1再現はGoのセクション5の教材そのものであり、Rust実装での
/// 主題ではないため効率的な実装にしている)
pub async fn list_tasks_offset(
    pool: &MySqlPool,
    user_id: u64,
    name: &str,
    status: Option<TaskStatus>,
    label_ids: &[u64],
    sort_finished_on: &str,
    limit: i64,
    offset: i64,
) -> Result<(Vec<Task>, i64), sqlx::Error> {
    let filter = build_filter(user_id, name, status, label_ids);

    let count_sql = format!("SELECT COUNT(*) AS cnt FROM tasks{}", filter.sql);
    let count_row = bind_all(sqlx::query(&count_sql), &filter.binds)
        .fetch_one(pool)
        .await?;
    let total: i64 = count_row.get("cnt");

    let order = match sort_finished_on {
        "asc" => "finished_on ASC",
        "desc" => "finished_on DESC",
        _ => "created_at DESC",
    };
    let list_sql = format!(
        "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at FROM tasks{} ORDER BY {} LIMIT ? OFFSET ?",
        filter.sql, order
    );
    let mut q = bind_all(sqlx::query(&list_sql), &filter.binds);
    q = q.bind(limit).bind(offset);
    let rows = q.fetch_all(pool).await?;

    let mut tasks: Vec<Task> = rows.into_iter().map(row_to_task).collect();
    attach_labels(pool, &mut tasks).await?;
    Ok((tasks, total))
}

/// v2(gRPC)向け: cursor(keyset)ページング
/// backend(Go)のUseCursor(id昇順固定)と同じ規約
pub async fn list_tasks_cursor(
    pool: &MySqlPool,
    user_id: u64,
    name: &str,
    status: Option<TaskStatus>,
    label_ids: &[u64],
    cursor: u64,
    limit: i64,
) -> Result<Vec<Task>, sqlx::Error> {
    let mut filter = build_filter(user_id, name, status, label_ids);
    if cursor > 0 {
        filter.sql.push_str(" AND id > ?");
        filter.binds.push(Bind::U64(cursor));
    }
    let list_sql = format!(
        "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at FROM tasks{} ORDER BY id ASC LIMIT ?",
        filter.sql
    );
    let mut q = bind_all(sqlx::query(&list_sql), &filter.binds);
    q = q.bind(limit);
    let rows = q.fetch_all(pool).await?;

    let mut tasks: Vec<Task> = rows.into_iter().map(row_to_task).collect();
    attach_labels(pool, &mut tasks).await?;
    Ok(tasks)
}

/// ユーザースコープ付きで1件取得(他人のtaskは見えない
/// backend(Go)のrepo.Getと同じ)
pub async fn get_task(pool: &MySqlPool, id: u64, user_id: u64) -> Result<Option<Task>, sqlx::Error> {
    let row = sqlx::query(
        "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at FROM tasks WHERE id = ? AND user_id = ?",
    )
    .bind(id)
    .bind(user_id)
    .fetch_optional(pool)
    .await?;

    match row {
        None => Ok(None),
        Some(row) => {
            let mut tasks = vec![row_to_task(row)];
            attach_labels(pool, &mut tasks).await?;
            Ok(tasks.into_iter().next())
        }
    }
}

pub async fn create_task(
    pool: &MySqlPool,
    user_id: u64,
    input: &TaskInput,
    status: TaskStatus,
) -> Result<u64, sqlx::Error> {
    let mut tx = pool.begin().await?;
    let now = Utc::now().naive_utc();
    let result = sqlx::query(
        "INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
    )
    .bind(&input.name)
    .bind(&input.description)
    .bind(status as u8)
    .bind(input.finished_on)
    .bind(user_id)
    .bind(now)
    .bind(now)
    .execute(&mut *tx)
    .await?;
    let task_id = result.last_insert_id();

    replace_labels(&mut tx, task_id, &input.label_ids, now).await?;
    tx.commit().await?;
    Ok(task_id)
}

/// 戻り値: 更新できた場合true、対象行が(他人のtaskも含め)見つからない場合false
pub async fn update_task(
    pool: &MySqlPool,
    id: u64,
    user_id: u64,
    input: &TaskInput,
    status: TaskStatus,
) -> Result<bool, sqlx::Error> {
    let mut tx = pool.begin().await?;
    let now = Utc::now().naive_utc();
    let result = sqlx::query(
        "UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, updated_at = ? WHERE id = ? AND user_id = ?",
    )
    .bind(&input.name)
    .bind(&input.description)
    .bind(status as u8)
    .bind(input.finished_on)
    .bind(now)
    .bind(id)
    .bind(user_id)
    .execute(&mut *tx)
    .await?;

    if result.rows_affected() == 0 {
        tx.rollback().await?;
        return Ok(false);
    }

    replace_labels(&mut tx, id, &input.label_ids, now).await?;
    tx.commit().await?;
    Ok(true)
}

pub async fn delete_task(pool: &MySqlPool, id: u64, user_id: u64) -> Result<bool, sqlx::Error> {
    let result = sqlx::query("DELETE FROM tasks WHERE id = ? AND user_id = ?")
        .bind(id)
        .bind(user_id)
        .execute(pool)
        .await?;
    Ok(result.rows_affected() > 0)
}

async fn replace_labels(
    tx: &mut sqlx::Transaction<'_, sqlx::MySql>,
    task_id: u64,
    label_ids: &[u64],
    now: NaiveDateTime,
) -> Result<(), sqlx::Error> {
    sqlx::query("DELETE FROM task_labels WHERE task_id = ?")
        .bind(task_id)
        .execute(&mut **tx)
        .await?;
    // 【テスト監査で発見・修正した実バグ】label_idsに同じidが重複して含まれる場合
    // (例: [3,3,5])、重複除去せずそのままINSERTすると2回目の(task_id,3)で
    // task_labelsの(task_id,label_id)へのUNIQUE制約(migrations/000004)に
    // 違反し、生のMySQLエラー(1062 Duplicate entry)がそのまま呼び出し元へ伝播してしまう
    // (Goのbackend/internal/repository/task.goで見つかった同種のバグと同じ根本原因)
    let mut seen = std::collections::HashSet::with_capacity(label_ids.len());
    for label_id in label_ids {
        if !seen.insert(*label_id) {
            continue;
        }
        sqlx::query(
            "INSERT INTO task_labels (task_id, label_id, created_at, updated_at) VALUES (?, ?, ?, ?)",
        )
        .bind(task_id)
        .bind(label_id)
        .bind(now)
        .bind(now)
        .execute(&mut **tx)
        .await?;
    }
    Ok(())
}

async fn attach_labels(pool: &MySqlPool, tasks: &mut [Task]) -> Result<(), sqlx::Error> {
    if tasks.is_empty() {
        return Ok(());
    }
    let ids: Vec<u64> = tasks.iter().map(|t| t.id).collect();
    let placeholders = ids.iter().map(|_| "?").collect::<Vec<_>>().join(",");
    let sql = format!(
        "SELECT task_labels.task_id AS task_id, labels.id AS id, labels.name AS name \
         FROM task_labels JOIN labels ON labels.id = task_labels.label_id \
         WHERE task_labels.task_id IN ({})",
        placeholders
    );
    let mut q = sqlx::query(&sql);
    for id in &ids {
        q = q.bind(*id);
    }
    let rows = q.fetch_all(pool).await?;

    let mut by_task: HashMap<u64, Vec<Label>> = HashMap::new();
    for row in rows {
        let task_id: u64 = row.get("task_id");
        let label = Label {
            id: row.get::<u64, _>("id"),
            name: row.get("name"),
        };
        by_task.entry(task_id).or_default().push(label);
    }

    for task in tasks.iter_mut() {
        if let Some(labels) = by_task.remove(&task.id) {
            task.labels = labels;
        }
    }
    Ok(())
}

/// 外部公開API v1(offset、CONTRACT.mdセクション11)向け
/// フィルタは持たず
/// user_idのみで絞り込む
/// backend(Go)のListOffsetForExternalAPIと同じソート順
/// (created_at DESC, id DESC)
pub async fn list_tasks_offset_external(
    pool: &MySqlPool,
    user_id: u64,
    page: i64,
    page_size: i64,
) -> Result<(Vec<Task>, i64), sqlx::Error> {
    let count_row = sqlx::query("SELECT COUNT(*) AS cnt FROM tasks WHERE user_id = ?")
        .bind(user_id)
        .fetch_one(pool)
        .await?;
    let total: i64 = count_row.get("cnt");

    let offset = (page - 1) * page_size;
    let rows = sqlx::query(
        "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at \
         FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
    )
    .bind(user_id)
    .bind(page_size)
    .bind(offset)
    .fetch_all(pool)
    .await?;

    let mut tasks: Vec<Task> = rows.into_iter().map(row_to_task).collect();
    attach_labels(pool, &mut tasks).await?;
    Ok((tasks, total))
}

/// 外部公開API v2(keyset/cursor、CONTRACT.mdセクション11)向け
/// backend(Go)のListCursorForExternalAPIと同じ絞り込み条件・ソート順
/// ((created_at, id)の複合条件でOFFSETを使わない)
pub async fn list_tasks_cursor_external(
    pool: &MySqlPool,
    user_id: u64,
    after: Option<(NaiveDateTime, u64)>,
    limit: i64,
) -> Result<Vec<Task>, sqlx::Error> {
    let rows = if let Some((created_at, id)) = after {
        sqlx::query(
            "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at \
             FROM tasks WHERE user_id = ? AND ((created_at < ?) OR (created_at = ? AND id < ?)) \
             ORDER BY created_at DESC, id DESC LIMIT ?",
        )
        .bind(user_id)
        .bind(created_at)
        .bind(created_at)
        .bind(id)
        .bind(limit)
        .fetch_all(pool)
        .await?
    } else {
        sqlx::query(
            "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at \
             FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?",
        )
        .bind(user_id)
        .bind(limit)
        .fetch_all(pool)
        .await?
    };

    let mut tasks: Vec<Task> = rows.into_iter().map(row_to_task).collect();
    attach_labels(pool, &mut tasks).await?;
    Ok(tasks)
}

fn row_to_task(row: sqlx::mysql::MySqlRow) -> Task {
    let status_raw: u8 = row.get("status");
    let finished_on: NaiveDate = row.get("finished_on");
    Task {
        id: row.get::<u64, _>("id"),
        name: row.get("name"),
        description: row.get("description"),
        status: TaskStatus::from_db(status_raw).unwrap_or(TaskStatus::Waiting),
        finished_on,
        user_id: row.get::<u64, _>("user_id"),
        created_at: row.get("created_at"),
        updated_at: row.get("updated_at"),
        labels: Vec::new(),
    }
}
