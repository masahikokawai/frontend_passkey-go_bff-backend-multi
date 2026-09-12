use std::sync::Arc;

use axum::extract::{Path, Query, State};
use axum::http::StatusCode;
use axum::response::IntoResponse;
use axum::Json;
use chrono::{NaiveDate, Utc};
use serde::Deserialize;
use serde_json::{json, Value};

use crate::error::RestError;
use crate::model::{validate_task_input, Task, TaskInput, TaskStatus};
use crate::{db, AppState};

use super::authenticate;

#[derive(Debug, Deserialize)]
pub struct ListQuery {
    #[serde(default)]
    name: String,
    #[serde(default)]
    status: String,
    #[serde(default)]
    label_ids: String,
    #[serde(default)]
    sort: String,
    limit: Option<i64>,
    offset: Option<i64>,
}

fn parse_label_ids(raw: &str) -> Vec<u64> {
    if raw.is_empty() {
        return Vec::new();
    }
    raw.split(',').filter_map(|p| p.trim().parse::<u64>().ok()).collect()
}

/// CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
/// 【backend(Go)の実際の挙動に合わせた既知の差異】taskDTOToJSON(backend/internal/handler/v1/task.go)は
/// user_idをレスポンスに含めていない(CONTRACT.md本文の例には書かれているが、実装はそうなっていない。
/// bff側のtaskV1DTOはuser_id用フィールドを持つが、無ければゼロ値のまま使われ実害はない)。
/// ここではワイヤー契約パリティの原則(セクション20.5)に従い、ドキュメントではなく
/// 実際のGo実装の挙動に合わせている
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

pub async fn list(
    State(state): State<Arc<AppState>>,
    headers: axum::http::HeaderMap,
    Query(q): Query<ListQuery>,
) -> Result<impl IntoResponse, RestError> {
    let user_id = authenticate(&state, &headers).await?;

    let status = if q.status.is_empty() {
        None
    } else {
        Some(TaskStatus::from_str(&q.status).ok_or(RestError::InvalidStatus)?)
    };
    let label_ids = parse_label_ids(&q.label_ids);
    let limit = q.limit.unwrap_or(20);
    let offset = q.offset.unwrap_or(0);

    let (tasks, total) = db::list_tasks_offset(&state.pool, user_id, &q.name, status, &label_ids, &q.sort, limit, offset)
        .await
        .map_err(|_| RestError::Internal)?;

    let tasks_json: Vec<Value> = tasks.iter().map(task_to_json).collect();
    Ok(Json(json!({"tasks": tasks_json, "total": total, "limit": limit, "offset": offset})))
}

pub async fn get(
    State(state): State<Arc<AppState>>,
    headers: axum::http::HeaderMap,
    Path(id): Path<String>,
) -> Result<impl IntoResponse, RestError> {
    let user_id = authenticate(&state, &headers).await?;
    let id: u64 = id.parse().map_err(|_| RestError::InvalidId)?;

    let task = db::get_task(&state.pool, id, user_id)
        .await
        .map_err(|_| RestError::Internal)?
        .ok_or(RestError::NotFound)?;
    Ok(Json(task_to_json(&task)))
}

#[derive(Debug, Deserialize, Default)]
pub struct TaskRequestBody {
    #[serde(default)]
    name: String,
    #[serde(default)]
    description: Option<String>,
    #[serde(default)]
    status: String,
    #[serde(default)]
    finished_on: String,
    #[serde(default)]
    label_ids: Vec<u64>,
}

/// backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い
fn to_task_input(body: TaskRequestBody) -> Result<(TaskInput, NaiveDate), RestError> {
    if body.name.is_empty() || body.status.is_empty() || body.finished_on.is_empty() {
        return Err(RestError::InvalidRequest);
    }
    let finished_on =
        NaiveDate::parse_from_str(&body.finished_on, "%Y-%m-%d").map_err(|_| RestError::InvalidFinishedOn)?;
    Ok((
        TaskInput {
            name: body.name,
            description: body.description,
            status: body.status,
            finished_on,
            label_ids: body.label_ids,
        },
        finished_on,
    ))
}

pub async fn create(
    State(state): State<Arc<AppState>>,
    headers: axum::http::HeaderMap,
    body: Result<Json<TaskRequestBody>, axum::extract::rejection::JsonRejection>,
) -> Result<impl IntoResponse, RestError> {
    let user_id = authenticate(&state, &headers).await?;
    let Json(body) = body.map_err(|_| RestError::InvalidRequest)?;
    let (input, _) = to_task_input(body)?;

    let today = Utc::now().date_naive();
    let status = validate_task_input(&input, today).map_err(RestError::Validation)?;

    let id = db::create_task(&state.pool, user_id, &input, status)
        .await
        .map_err(|_| RestError::Internal)?;
    let task = db::get_task(&state.pool, id, user_id)
        .await
        .map_err(|_| RestError::Internal)?
        .ok_or(RestError::Internal)?;
    Ok((StatusCode::CREATED, Json(task_to_json(&task))))
}

pub async fn update(
    State(state): State<Arc<AppState>>,
    headers: axum::http::HeaderMap,
    Path(id): Path<String>,
    body: Result<Json<TaskRequestBody>, axum::extract::rejection::JsonRejection>,
) -> Result<impl IntoResponse, RestError> {
    let user_id = authenticate(&state, &headers).await?;
    let id: u64 = id.parse().map_err(|_| RestError::InvalidId)?;
    let Json(body) = body.map_err(|_| RestError::InvalidRequest)?;
    let (input, _) = to_task_input(body)?;

    let today = Utc::now().date_naive();
    let status = validate_task_input(&input, today).map_err(RestError::Validation)?;

    let updated = db::update_task(&state.pool, id, user_id, &input, status)
        .await
        .map_err(|_| RestError::Internal)?;
    if !updated {
        return Err(RestError::NotFound);
    }
    let task = db::get_task(&state.pool, id, user_id)
        .await
        .map_err(|_| RestError::Internal)?
        .ok_or(RestError::NotFound)?;
    Ok(Json(task_to_json(&task)))
}

pub async fn delete(
    State(state): State<Arc<AppState>>,
    headers: axum::http::HeaderMap,
    Path(id): Path<String>,
) -> Result<impl IntoResponse, RestError> {
    let user_id = authenticate(&state, &headers).await?;
    let id: u64 = id.parse().map_err(|_| RestError::InvalidId)?;

    let deleted = db::delete_task(&state.pool, id, user_id)
        .await
        .map_err(|_| RestError::Internal)?;
    if !deleted {
        return Err(RestError::NotFound);
    }
    Ok(StatusCode::NO_CONTENT)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::Label;
    use chrono::NaiveDateTime;

    #[test]
    fn parse_label_ids_splits_on_comma_and_ignores_garbage() {
        assert_eq!(parse_label_ids(""), Vec::<u64>::new());
        assert_eq!(parse_label_ids("1,2,3"), vec![1, 2, 3]);
        assert_eq!(parse_label_ids(" 1 , 2 "), vec![1, 2]);
        // backend(Go)のparseUintListQueryと同じく、パースに失敗した要素は無視して続行する
        assert_eq!(parse_label_ids("1,x,3"), vec![1, 3]);
    }

    // CONTRACT.mdセクション5.1のJSON形状(スネークケース、user_idは含めない)と
    // 完全に一致することを確認する
    #[test]
    fn task_to_json_matches_contract_shape() {
        let task = Task {
            id: 1,
            name: "buy milk".to_string(),
            description: None,
            status: TaskStatus::Waiting,
            finished_on: NaiveDate::from_ymd_opt(2026, 9, 6).unwrap(),
            user_id: 999,
            created_at: NaiveDateTime::parse_from_str("2026-09-06 12:00:00", "%Y-%m-%d %H:%M:%S").unwrap(),
            updated_at: NaiveDateTime::parse_from_str("2026-09-06 12:00:00", "%Y-%m-%d %H:%M:%S").unwrap(),
            labels: vec![Label { id: 5, name: "urgent".to_string() }],
        };

        let got = task_to_json(&task);
        assert_eq!(got["id"], json!(1));
        assert_eq!(got["name"], json!("buy milk"));
        assert_eq!(got["description"], json!(null));
        assert_eq!(got["status"], json!("waiting"));
        assert_eq!(got["finished_on"], json!("2026-09-06"));
        assert_eq!(got["labels"], json!([{"id": 5, "name": "urgent"}]));
        // user_idキーは意図的に含めない(backend(Go)の実際の挙動、コメント参照)
        assert!(got.get("user_id").is_none());
        assert!(got["created_at"].as_str().unwrap().starts_with("2026-09-06T12:00:00"));
    }

    #[test]
    fn to_task_input_rejects_empty_required_fields() {
        let body = TaskRequestBody { name: "".to_string(), ..Default::default() };
        assert!(matches!(to_task_input(body), Err(RestError::InvalidRequest)));

        let body = TaskRequestBody { name: "x".to_string(), status: "".to_string(), finished_on: "2026-09-06".to_string(), ..Default::default() };
        assert!(matches!(to_task_input(body), Err(RestError::InvalidRequest)));

        let body = TaskRequestBody { name: "x".to_string(), status: "waiting".to_string(), finished_on: "".to_string(), ..Default::default() };
        assert!(matches!(to_task_input(body), Err(RestError::InvalidRequest)));
    }

    #[test]
    fn to_task_input_rejects_unparseable_finished_on() {
        let body = TaskRequestBody {
            name: "x".to_string(),
            status: "waiting".to_string(),
            finished_on: "not-a-date".to_string(),
            ..Default::default()
        };
        assert!(matches!(to_task_input(body), Err(RestError::InvalidFinishedOn)));
    }

    #[test]
    fn to_task_input_accepts_valid_body() {
        let body = TaskRequestBody {
            name: "x".to_string(),
            status: "waiting".to_string(),
            finished_on: "2026-09-06".to_string(),
            description: Some("d".to_string()),
            label_ids: vec![1, 2],
        };
        let (input, finished_on) = to_task_input(body).expect("should parse");
        assert_eq!(input.name, "x");
        assert_eq!(input.label_ids, vec![1, 2]);
        assert_eq!(finished_on, NaiveDate::from_ymd_opt(2026, 9, 6).unwrap());
    }
}
