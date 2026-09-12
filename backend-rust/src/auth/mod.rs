pub mod jwt;

use sqlx::mysql::MySqlPool;

use jwt::Claims;

/// backend(Go)のresolveUserID(v1/task.go・grpcserver/task_service.go)と同じ分岐:
///   - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの → usersをidで検索
///   - Keycloak発行のJWT: subはkeycloak_sub → users.keycloak_subで検索
/// どちらも見つからなければUserNotProvisioned(REST側403 user_not_provisioned /
/// gRPC側 codes::PermissionDenied "user not provisioned" に対応する)
pub enum ResolveOutcome {
    Ok(u64),
    UserNotProvisioned,
}

pub async fn resolve_user_id(pool: &MySqlPool, claims: &Claims) -> ResolveOutcome {
    if jwt::is_local_issuer(&claims.iss) {
        let id: u64 = match claims.sub.parse() {
            Ok(v) => v,
            Err(_) => return ResolveOutcome::UserNotProvisioned,
        };
        return match crate::db::find_user_by_id(pool, id).await {
            Some(user) => ResolveOutcome::Ok(user.id),
            None => ResolveOutcome::UserNotProvisioned,
        };
    }

    match crate::db::find_user_by_keycloak_sub(pool, &claims.sub).await {
        Some(user) => ResolveOutcome::Ok(user.id),
        None => ResolveOutcome::UserNotProvisioned,
    }
}
