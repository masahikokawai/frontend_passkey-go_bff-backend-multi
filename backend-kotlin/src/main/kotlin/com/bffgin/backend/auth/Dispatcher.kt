package com.bffgin.backend.auth

import com.fasterxml.jackson.databind.ObjectMapper
import java.util.Base64
import java.util.concurrent.ConcurrentHashMap

/**
 * JWTの`iss`クレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
 * (backend(Go)のauthjwt.Dispatcher・backend-rust/backend-c/backend-cpp/backend-javaと
 * 同じ2段構造)。issの詐称は委譲先の署名検証で弾かれる: 「振り分けのために覗く」ことと
 * 「検証を信頼する」ことは別、という2段階構造を維持する
 */
class Dispatcher {

    companion object {
        const val LOCAL_HMAC_ISSUER = "bff-gin-local-hmac"
        const val LOCAL_RSA_ISSUER = "bff-gin-local-rsa"

        fun isLocalIssuer(iss: String): Boolean = iss == LOCAL_HMAC_ISSUER || iss == LOCAL_RSA_ISSUER

        private fun padBase64Url(s: String): String {
            val rem = s.length % 4
            return if (rem == 0) s else s + "=".repeat(4 - rem)
        }
    }

    private val byIssuer = ConcurrentHashMap<String, TokenVerifier>()
    private val mapper = ObjectMapper()

    fun register(issuer: String, verifier: TokenVerifier): Dispatcher {
        byIssuer[issuer] = verifier
        return this
    }

    suspend fun verify(token: String): Claims {
        val parts = token.split(".")
        if (parts.size != 3) {
            throw VerifyException("malformed JWT")
        }
        val iss: String
        try {
            val payload = Base64.getUrlDecoder().decode(padBase64Url(parts[1]))
            val json = mapper.readTree(payload)
            iss = json.get("iss")?.asText() ?: throw VerifyException("unknown issuer: null")
        } catch (e: VerifyException) {
            throw e
        } catch (e: Exception) {
            throw VerifyException("failed to peek iss claim", e)
        }

        val verifier = byIssuer[iss] ?: throw VerifyException("unknown issuer: $iss")
        return verifier.verify(token)
    }
}
