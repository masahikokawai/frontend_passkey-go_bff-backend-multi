use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::Json;
use serde_json::json;

/// backend(Go)の internal/handler/v1/render.go の renderServiceError と
/// 各ハンドラのエラー分岐(invalid_request/invalid_status/invalid_finished_on)を1つのenumへまとめたもの
/// JSON形状・ステータスコードは1文字も変えていない
#[derive(Debug)]
pub enum RestError {
    Unauthorized,
    UserNotProvisioned,
    InvalidRequest,
    InvalidId,
    InvalidStatus,
    InvalidFinishedOn,
    Validation(String),
    NotFound,
    Internal,
    // 外部公開API(CONTRACT.mdセクション11)専用
    // backend(Go)の authjwt.RequireExternalClientAuth・handler/external/task.go と対応する
    InvalidToken,
    ClientNotAllowed,
    UserIdRequired,
    InvalidUserId,
    InvalidCursor(String),
}

impl IntoResponse for RestError {
    fn into_response(self) -> Response {
        let (status, body) = match self {
            RestError::Unauthorized => (StatusCode::UNAUTHORIZED, json!({"error": "unauthorized"})),
            RestError::UserNotProvisioned => {
                (StatusCode::FORBIDDEN, json!({"error": "user_not_provisioned"}))
            }
            RestError::InvalidRequest => (StatusCode::BAD_REQUEST, json!({"error": "invalid_request"})),
            RestError::InvalidId => (StatusCode::BAD_REQUEST, json!({"error": "invalid_id"})),
            RestError::InvalidStatus => {
                (StatusCode::UNPROCESSABLE_ENTITY, json!({"error": "invalid_status"}))
            }
            RestError::InvalidFinishedOn => {
                (StatusCode::UNPROCESSABLE_ENTITY, json!({"error": "invalid_finished_on"}))
            }
            RestError::Validation(msg) => (
                StatusCode::UNPROCESSABLE_ENTITY,
                json!({"error": "validation_error", "message": msg}),
            ),
            RestError::NotFound => (StatusCode::NOT_FOUND, json!({"error": "not_found"})),
            RestError::Internal => (
                StatusCode::INTERNAL_SERVER_ERROR,
                json!({"error": "internal_server_error"}),
            ),
            RestError::InvalidToken => (StatusCode::UNAUTHORIZED, json!({"error": "invalid_token"})),
            RestError::ClientNotAllowed => {
                (StatusCode::FORBIDDEN, json!({"error": "client_not_allowed"}))
            }
            RestError::UserIdRequired => {
                (StatusCode::BAD_REQUEST, json!({"error": "user_id is required"}))
            }
            RestError::InvalidUserId => {
                (StatusCode::BAD_REQUEST, json!({"error": "invalid user_id"}))
            }
            RestError::InvalidCursor(msg) => (
                StatusCode::UNPROCESSABLE_ENTITY,
                json!({"error": format!("validation_error: {}", msg)}),
            ),
        };
        (status, Json(body)).into_response()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::to_bytes;

    async fn status_and_body(err: RestError) -> (StatusCode, serde_json::Value) {
        let resp = err.into_response();
        let status = resp.status();
        let bytes = to_bytes(resp.into_body(), usize::MAX).await.unwrap();
        let body: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
        (status, body)
    }

    // backend(Go)の internal/handler/v1/render.go・task.go のエラー分岐と
    // 1対1で対応することを確認する(セクション20.5のワイヤー契約パリティ)
    #[tokio::test]
    async fn unauthorized_maps_to_401() {
        let (status, body) = status_and_body(RestError::Unauthorized).await;
        assert_eq!(status, StatusCode::UNAUTHORIZED);
        assert_eq!(body, json!({"error": "unauthorized"}));
    }

    #[tokio::test]
    async fn user_not_provisioned_maps_to_403() {
        let (status, body) = status_and_body(RestError::UserNotProvisioned).await;
        assert_eq!(status, StatusCode::FORBIDDEN);
        assert_eq!(body, json!({"error": "user_not_provisioned"}));
    }

    #[tokio::test]
    async fn invalid_request_maps_to_400() {
        let (status, body) = status_and_body(RestError::InvalidRequest).await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body, json!({"error": "invalid_request"}));
    }

    #[tokio::test]
    async fn invalid_id_maps_to_400() {
        let (status, body) = status_and_body(RestError::InvalidId).await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body, json!({"error": "invalid_id"}));
    }

    #[tokio::test]
    async fn invalid_status_maps_to_422() {
        let (status, body) = status_and_body(RestError::InvalidStatus).await;
        assert_eq!(status, StatusCode::UNPROCESSABLE_ENTITY);
        assert_eq!(body, json!({"error": "invalid_status"}));
    }

    #[tokio::test]
    async fn invalid_finished_on_maps_to_422() {
        let (status, body) = status_and_body(RestError::InvalidFinishedOn).await;
        assert_eq!(status, StatusCode::UNPROCESSABLE_ENTITY);
        assert_eq!(body, json!({"error": "invalid_finished_on"}));
    }

    #[tokio::test]
    async fn validation_maps_to_422_with_message() {
        let (status, body) = status_and_body(RestError::Validation("nameは必須です".to_string())).await;
        assert_eq!(status, StatusCode::UNPROCESSABLE_ENTITY);
        assert_eq!(body, json!({"error": "validation_error", "message": "nameは必須です"}));
    }

    #[tokio::test]
    async fn not_found_maps_to_404() {
        let (status, body) = status_and_body(RestError::NotFound).await;
        assert_eq!(status, StatusCode::NOT_FOUND);
        assert_eq!(body, json!({"error": "not_found"}));
    }

    #[tokio::test]
    async fn internal_maps_to_500() {
        let (status, body) = status_and_body(RestError::Internal).await;
        assert_eq!(status, StatusCode::INTERNAL_SERVER_ERROR);
        assert_eq!(body, json!({"error": "internal_server_error"}));
    }

    #[tokio::test]
    async fn invalid_token_maps_to_401() {
        let (status, body) = status_and_body(RestError::InvalidToken).await;
        assert_eq!(status, StatusCode::UNAUTHORIZED);
        assert_eq!(body, json!({"error": "invalid_token"}));
    }

    #[tokio::test]
    async fn client_not_allowed_maps_to_403() {
        let (status, body) = status_and_body(RestError::ClientNotAllowed).await;
        assert_eq!(status, StatusCode::FORBIDDEN);
        assert_eq!(body, json!({"error": "client_not_allowed"}));
    }

    #[tokio::test]
    async fn user_id_required_maps_to_400() {
        let (status, body) = status_and_body(RestError::UserIdRequired).await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body, json!({"error": "user_id is required"}));
    }

    #[tokio::test]
    async fn invalid_user_id_maps_to_400() {
        let (status, body) = status_and_body(RestError::InvalidUserId).await;
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(body, json!({"error": "invalid user_id"}));
    }

    #[tokio::test]
    async fn invalid_cursor_maps_to_422() {
        let (status, _) = status_and_body(RestError::InvalidCursor("bad".to_string())).await;
        assert_eq!(status, StatusCode::UNPROCESSABLE_ENTITY);
    }
}
