use std::sync::Arc;

use axum::extract::{Query, State};
use axum::http::HeaderMap;
use axum::response::IntoResponse;
use axum::Json;
use serde::Deserialize;
use serde_json::{json, Value};

use crate::db;
use crate::error::RestError;
use crate::model::Task;

use super::{authenticate, cursor, ExternalState};

/// backend(Go)のtaskDTOToJSON(internal/handler/external/task.go)と同じ形状。
/// 内部CRUDのレスポンスとは異なり**user_idを含めない**点に注意
fn task_to_json(task: &Task) -> Value {
    let labels: Vec<Value> = task
        .labels
        .iter()
        .map(|l| json!({"id": l.id, "name": l.name}))
        .collect();
    json!({
        "id": task.id,
        "name": task.name,
        "description": task.description,
        "status": task.status.as_str(),
        "finished_on": task.finished_on.format("%Y-%m-%d").to_string(),
        "labels": labels,
        "created_at": task.created_at.and_utc().to_rfc3339(),
        "updated_at": task.updated_at.and_utc().to_rfc3339(),
    })
}

#[derive(Debug, Deserialize)]
pub struct ListQuery {
    user_id: Option<String>,
    // v1(offset)
    page: Option<i64>,
    page_size: Option<i64>,
    // v2(cursor)
    cursor: Option<String>,
    limit: Option<i64>,
}

/// GET /external/v1/tasks
/// `backend.external-tasks-pagination-v2`(5言語で共有する1つのFeature Flag)のON/OFFで
/// offsetページング(v1)/keysetページング(v2)を切り替える(backend(Go)のTaskHandler.Listと同じ)
pub async fn list(
    State(state): State<Arc<ExternalState>>,
    headers: HeaderMap,
    Query(q): Query<ListQuery>,
) -> Result<impl IntoResponse, RestError> {
    authenticate(&state, &headers).await?;

    let user_id_str = q.user_id.ok_or(RestError::UserIdRequired)?;
    if user_id_str.is_empty() {
        return Err(RestError::UserIdRequired);
    }
    let user_id: u64 = user_id_str.parse().map_err(|_| RestError::InvalidUserId)?;

    let use_v2 = state.pagination_v2.get();
    if use_v2 {
        list_v2(&state, user_id, q.cursor, q.limit).await
    } else {
        list_v1(&state, user_id, q.page, q.page_size).await
    }
}

async fn list_v1(
    state: &ExternalState,
    user_id: u64,
    page: Option<i64>,
    page_size: Option<i64>,
) -> Result<Json<Value>, RestError> {
    let page = page.filter(|p| *p >= 1).unwrap_or(1);
    let page_size = page_size.filter(|p| *p >= 1).unwrap_or(10);

    let (tasks, total) = db::list_tasks_offset_external(&state.pool, user_id, page, page_size)
        .await
        .map_err(|_| RestError::Internal)?;

    let tasks_json: Vec<Value> = tasks.iter().map(task_to_json).collect();
    Ok(Json(json!({
        "tasks": tasks_json,
        "page": page,
        "page_size": page_size,
        "total": total,
    })))
}

async fn list_v2(
    state: &ExternalState,
    user_id: u64,
    cursor_str: Option<String>,
    limit: Option<i64>,
) -> Result<Json<Value>, RestError> {
    let limit = limit.filter(|l| *l >= 1).unwrap_or(10);

    let after = match cursor_str.filter(|c| !c.is_empty()) {
        Some(c) => Some(cursor::decode(&c).map_err(RestError::InvalidCursor)?),
        None => None,
    };

    let tasks = db::list_tasks_cursor_external(&state.pool, user_id, after, limit)
        .await
        .map_err(|_| RestError::Internal)?;

    let next_cursor: Value = if tasks.len() as i64 == limit {
        if let Some(last) = tasks.last() {
            json!(cursor::encode(last.created_at, last.id))
        } else {
            Value::Null
        }
    } else {
        Value::Null
    };

    let tasks_json: Vec<Value> = tasks.iter().map(task_to_json).collect();
    Ok(Json(json!({
        "tasks": tasks_json,
        "next_cursor": next_cursor,
        "limit": limit,
    })))
}
