package com.bffgin.backend.auth

import com.bffgin.backend.rest.ExternalApiError

/**
 * 外部公開API専用の認証(backend(Go)のRequireExternalClientAuth・backend-c/backend-cppの
 * auth_require_external_client/RequireExternalClientAuth相当)。内部REST/gRPCのUserResolverとは
 * 別の判定基準を使う:
 *   1. まず通常のDispatcherで署名検証(issで振り分け、実際の検証は委譲先のVerifierが行う)
 *   2. ローカル(HMAC/RSA)発行のissは拒否する(Client Credentials Grant、つまりKeycloak発行分のみ許可)
 *   3. azp(authorized party)クレームがexternalApiClientIdと一致することを要求する
 * user_idはクエリパラメータをそのまま使い、JWTのsubとは突き合わせない
 * (この資格情報を持つ者は任意ユーザーのタスクを読み取れる、CONTRACT.mdセクション11に明記された既知の設計)
 */
object ExternalAuth {
    suspend fun requireExternalClient(
        authorizationHeader: String?,
        dispatcher: Dispatcher,
        externalApiClientId: String,
    ): Claims {
        if (authorizationHeader == null || !authorizationHeader.startsWith("Bearer ")) {
            throw ExternalApiError.unauthenticated()
        }
        val token = authorizationHeader.substring("Bearer ".length)

        val claims = try {
            dispatcher.verify(token)
        } catch (e: VerifyException) {
            throw ExternalApiError.unauthenticated()
        }

        if (Dispatcher.isLocalIssuer(claims.iss)) {
            throw ExternalApiError.unauthenticated()
        }
        if (claims.azp != externalApiClientId) {
            throw ExternalApiError.unauthenticated()
        }
        return claims
    }
}
