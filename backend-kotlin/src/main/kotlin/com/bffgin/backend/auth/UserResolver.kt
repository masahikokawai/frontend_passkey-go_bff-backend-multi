package com.bffgin.backend.auth

import com.bffgin.backend.domain.TaskError
import com.bffgin.backend.repository.TaskRepository
import org.slf4j.LoggerFactory

/**
 * REST/gRPCの両トランスポートが共有するuser_id解決ロジック(認証ロジックを複製しない設計)。
 * backend(Go)のresolveUserID・backend-rust/backend-javaのUserResolverと同じ分岐:
 *   - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの → usersをidで検索
 *   - Keycloak発行のJWT: subはkeycloak_sub → user_keycloaks経由で検索
 * どちらも見つからなければUserNotProvisioned。
 * repositoryのメソッドは内部でwithContext(Dispatchers.IO)を使うため、ここで改めて
 * ディスパッチャを意識する必要は無い(呼び出し元は「これはsuspend fun」と知るだけでよい)
 */
class UserResolver(
    private val dispatcher: Dispatcher,
    private val repository: TaskRepository,
) {
    private val log = LoggerFactory.getLogger(UserResolver::class.java)

    suspend fun resolve(authorizationHeader: String?): Long {
        if (authorizationHeader == null || !authorizationHeader.startsWith("Bearer ")) {
            throw TaskError.unauthorized()
        }
        val token = authorizationHeader.substring("Bearer ".length)

        val claims = try {
            dispatcher.verify(token)
        } catch (e: VerifyException) {
            throw TaskError.unauthorized()
        }

        val userId = if (Dispatcher.isLocalIssuer(claims.iss)) {
            val id = claims.sub.toLongOrNull() ?: throw TaskError.userNotProvisioned()
            val user = repository.findUserById(id) ?: throw TaskError.userNotProvisioned()
            user.id
        } else {
            val user = repository.findUserByKeycloakSub(claims.sub) ?: throw TaskError.userNotProvisioned()
            user.id
        }
        // LOG_LEVEL=debugのときのみ出るリクエスト単位の詳細ログ(要約行はMain.ktのINFOログが担う)
        log.debug("resolved user_id={} from iss={} sub={}", userId, claims.iss, claims.sub)
        return userId
    }
}
