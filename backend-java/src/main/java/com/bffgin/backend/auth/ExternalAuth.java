package com.bffgin.backend.auth;

import com.bffgin.backend.domain.TaskError;

/**
 * 外部公開API(CONTRACT.mdセクション11)向けの認証。内部REST/gRPCと同じDispatcherで
 * 署名検証まで行うが、ここではさらに2点を追加で要求する(backend-c/backend-cpp/backend-rustの
 * RequireExternalClientAuthと同じ設計):
 *   1. ローカル(HMAC/RSA)発行のissuerは拒否する(Client Credentials Grant、
 *      つまりKeycloak発行のトークンのみを受け付ける)
 *   2. `azp`(authorized party)クレームが期待するクライアントidと一致すること
 * user_id自体はこのクレームから解決せず、呼び出し側がクエリパラメータで指定した値を
 * そのまま信頼する(サーバー間の信頼関係を前提にした設計、CONTRACT.md参照)
 */
public final class ExternalAuth {

    private ExternalAuth() {
    }

    public static void requireExternalClient(Dispatcher dispatcher, String authorizationHeader,
            String externalApiClientId) throws TaskError {
        if (authorizationHeader == null || !authorizationHeader.startsWith("Bearer ")) {
            throw TaskError.unauthorized();
        }
        String token = authorizationHeader.substring("Bearer ".length());

        Claims claims;
        try {
            claims = dispatcher.verify(token);
        } catch (VerifyException e) {
            throw TaskError.unauthorized();
        }

        if (Dispatcher.isLocalIssuer(claims.iss())) {
            throw TaskError.unauthorized();
        }
        if (!externalApiClientId.equals(claims.azp())) {
            throw TaskError.unauthorized();
        }
    }
}
