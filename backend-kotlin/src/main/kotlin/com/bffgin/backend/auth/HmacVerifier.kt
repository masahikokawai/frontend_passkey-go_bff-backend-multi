package com.bffgin.backend.auth

import io.jsonwebtoken.JwtException
import io.jsonwebtoken.Jwts
import io.jsonwebtoken.security.Keys
import java.nio.charset.StandardCharsets
import javax.crypto.SecretKey

/** ローカルHMAC発行(iss=Dispatcher.LOCAL_HMAC_ISSUER)の検証。HS256、共有シークレット */
class HmacVerifier(secret: String, private val issuer: String, private val audience: String) : TokenVerifier {

    private val key: SecretKey = Keys.hmacShaKeyFor(secret.toByteArray(StandardCharsets.UTF_8))

    override suspend fun verify(token: String): Claims {
        try {
            val jws = Jwts.parser()
                .verifyWith(key)
                .build()
                .parseSignedClaims(token)

            // 【セキュリティ上の確認、backend-javaと同じ観点】jjwtはverifyWith()に渡した鍵の型
            // (SecretKey=HMAC系)と整合しないアルゴリズム(RS256等)のトークンをそもそも受理しないが、
            // ヘッダのalgが期待通りHS256であることも明示的に確認する(多層防御)
            val alg = jws.header.algorithm
            if (alg != "HS256") {
                throw VerifyException("unexpected alg: $alg")
            }

            val claims = jws.payload
            if (claims.issuer != issuer) {
                throw VerifyException("issuer mismatch")
            }
            if (!AudienceMatcher.matches(claims, audience)) {
                throw VerifyException("audience mismatch")
            }
            return Claims(claims.subject, claims.issuer, claims.get("azp", String::class.java) ?: "")
        } catch (e: VerifyException) {
            throw e
        } catch (e: JwtException) {
            throw VerifyException("HMAC verify failed: ${e.message}", e)
        }
    }
}
