pub mod cursor;
pub mod task;

use std::sync::Arc;

use axum::http::HeaderMap;
use axum::routing::get;
use axum::Router;

use crate::auth::jwt::{JwksVerifier, TokenVerifier};
use crate::error::RestError;
use crate::flags::FlagCache;
use sqlx::mysql::MySqlPool;

/// ExternalState は内部CRUD用のAppStateとは別に持つ。
/// 外部公開API(CONTRACT.mdセクション11)はKeycloak発行のClient Credentials Grantトークン
/// しか扱わない(ローカルHMAC/RSAは対象外)ため、3issuer分のDispatcherではなく
/// Keycloak向けのJwksVerifier単体で十分
pub struct ExternalState {
    pub pool: MySqlPool,
    pub keycloak_verifier: JwksVerifier,
    pub expected_client_id: String,
    pub pagination_v2: Arc<FlagCache>,
}

pub fn router(state: Arc<ExternalState>) -> Router {
    Router::new()
        .route("/external/v1/tasks", get(task::list))
        // リクエスト単位のログ(crate::logging参照)。method/path/status/durationを既定のINFOで出す
        .layer(axum::middleware::from_fn(crate::logging::log_requests))
        .with_state(state)
}

/// backend(Go)のauthjwt.RequireExternalClientAuthと同じ2段チェック:
/// (1)KeycloakのJWKSで署名検証、(2)`azp`クレームが期待するクライアントIDと一致するか
pub async fn authenticate(state: &ExternalState, headers: &HeaderMap) -> Result<(), RestError> {
    let auth_header = headers
        .get(axum::http::header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .ok_or(RestError::Unauthorized)?;
    let token = auth_header
        .strip_prefix("Bearer ")
        .filter(|t| !t.is_empty())
        .ok_or(RestError::Unauthorized)?;

    let claims = state
        .keycloak_verifier
        .verify(token)
        .await
        .map_err(|_| RestError::InvalidToken)?;

    if claims.azp != state.expected_client_id {
        return Err(RestError::ClientNotAllowed);
    }
    Ok(())
}
