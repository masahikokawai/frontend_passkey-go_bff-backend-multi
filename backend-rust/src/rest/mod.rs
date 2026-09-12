pub mod task;

use std::sync::Arc;

use axum::http::HeaderMap;
use axum::routing::get;
use axum::Router;

use crate::auth::{resolve_user_id, ResolveOutcome};
use crate::error::RestError;
use crate::AppState;

pub fn router(state: Arc<AppState>) -> Router {
    Router::new()
        .route("/internal/v1/tasks", get(task::list).post(task::create))
        .route(
            "/internal/v1/tasks/:id",
            get(task::get).patch(task::update).delete(task::delete),
        )
        // リクエスト単位のログ(crate::logging参照)。method/path/status/durationを既定のINFOで出す
        .layer(axum::middleware::from_fn(crate::logging::log_requests))
        .with_state(state)
}

/// backend(Go)のresolveUserID(REST v1版)と同じ: Authorizationヘッダを検証し、
/// 内部user_idを解決する。JWT無し/検証失敗は401 unauthorized、
/// 検証は通るがusersに該当行が無い場合は403 user_not_provisioned
pub async fn authenticate(state: &AppState, headers: &HeaderMap) -> Result<u64, RestError> {
    let auth_header = headers
        .get(axum::http::header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .ok_or(RestError::Unauthorized)?;
    let token = auth_header
        .strip_prefix("Bearer ")
        .ok_or(RestError::Unauthorized)?;

    let claims = state
        .dispatcher
        .verify(token)
        .await
        .map_err(|_| RestError::Unauthorized)?;

    match resolve_user_id(&state.pool, &claims).await {
        ResolveOutcome::Ok(user_id) => Ok(user_id),
        ResolveOutcome::UserNotProvisioned => Err(RestError::UserNotProvisioned),
    }
}
