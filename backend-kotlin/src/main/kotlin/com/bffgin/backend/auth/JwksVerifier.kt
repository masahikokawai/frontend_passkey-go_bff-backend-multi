package com.bffgin.backend.auth

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import io.jsonwebtoken.JwtException
import io.jsonwebtoken.Jwts
import io.ktor.client.HttpClient
import io.ktor.client.engine.cio.CIO
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.request.get
import io.ktor.client.statement.bodyAsText
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.slf4j.LoggerFactory
import java.math.BigInteger
import java.security.KeyFactory
import java.security.PublicKey
import java.security.spec.RSAPublicKeySpec
import java.util.Base64

/**
 * Keycloak発行・ローカルRSA発行(iss=Dispatcher.LOCAL_RSA_ISSUER)共通のJWKSベース検証。RS256。
 * kid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけJWKSを再取得する
 * (backend(Go)のjwks.go・backend-rust/backend-c/backend-cpp/backend-javaと同じ
 * 「kid不一致時のみ再取得」戦略)。
 *
 * 【コルーチンネイティブなHTTP呼び出し】JWKS取得はKtor Client(io.ktor.client、suspend fun get)を
 * 使う。java.net.http.HttpClientを同期呼び出しすると、その1箇所だけコルーチンディスパッチャの
 * スレッドを実際にブロックしてしまい、「ルーティングからDBアクセスまでコルーチンネイティブに
 * 貫く」という設計が崩れる(README.md「アーキテクチャ選定」節参照)
 */
class JwksVerifier(
    private val jwksUrl: String,
    private val issuer: String,
    private val audience: String,
) : TokenVerifier {

    private val http = HttpClient(CIO) {
        install(HttpTimeout) {
            requestTimeoutMillis = 5000
            connectTimeoutMillis = 5000
        }
    }
    private val mapper = ObjectMapper()
    private val refreshMutex = Mutex()
    private val log = LoggerFactory.getLogger(JwksVerifier::class.java)

    @Volatile
    private var keys: Map<String, PublicKey> = emptyMap()

    override suspend fun verify(token: String): Claims {
        val parts = token.split(".")
        if (parts.size != 3) {
            throw VerifyException("malformed token")
        }
        val kid = extractKid(parts[0]) ?: throw VerifyException("JWT header has no kid")

        var key = keys[kid]
        if (key == null) {
            log.debug("kid={} not in cache (issuer={}), refreshing JWKS from {}", kid, issuer, jwksUrl)
            refresh()
            key = keys[kid] ?: throw VerifyException("no public key found for kid=$kid")
        } else {
            log.debug("kid={} cache hit (issuer={})", kid, issuer)
        }

        try {
            val jws = Jwts.parser()
                .verifyWith(key)
                .build()
                .parseSignedClaims(token)

            val alg = jws.header.algorithm
            if (alg != "RS256") {
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
            throw VerifyException("RSA/JWKS verify failed: ${e.message}", e)
        }
    }

    private fun extractKid(headerB64: String): String? {
        return try {
            val raw = Base64.getUrlDecoder().decode(padBase64Url(headerB64))
            val header = mapper.readTree(raw)
            header.get("kid")?.asText()
        } catch (e: Exception) {
            throw VerifyException("failed to parse JWT header", e)
        }
    }

    private suspend fun refresh() {
        refreshMutex.withLock {
            try {
                val response = http.get(jwksUrl)
                if (response.status != HttpStatusCode.OK) {
                    throw VerifyException("JWKS fetch failed: HTTP ${response.status.value}")
                }
                val root = mapper.readTree(response.bodyAsText())
                val newKeys = mutableMapOf<String, PublicKey>()
                val keysArray: com.fasterxml.jackson.databind.node.ArrayNode = root.withArray("keys")
                for (k: JsonNode in keysArray) {
                    val kty = k.path("kty").asText("")
                    val use = k.path("use").asText("")
                    if (kty != "RSA" || (use.isNotEmpty() && use != "sig")) {
                        continue
                    }
                    val kid = k.path("kid").asText("")
                    val n = k.path("n").asText("")
                    val e = k.path("e").asText("")
                    if (kid.isEmpty() || n.isEmpty() || e.isEmpty()) {
                        continue
                    }
                    try {
                        newKeys[kid] = buildRsaPublicKey(n, e)
                    } catch (ex: Exception) {
                        // 【バッド/グッドプラクティス】特定の鍵1件の構築に失敗しただけで
                        // JWKS取得全体を失敗させると、他の正常な鍵まで使えなくなってしまう
                        // (1つの壊れたエントリが全体を巻き込む)。ここでは該当エントリだけ
                        // スキップし、他の鍵は正常にキャッシュへ反映する(backend-javaと同じ設計)
                        continue
                    }
                }
                keys = newKeys
                log.debug("JWKS refresh from {} succeeded, cached {} key(s)", jwksUrl, newKeys.size)
            } catch (e: VerifyException) {
                throw e
            } catch (e: Exception) {
                throw VerifyException("JWKS fetch failed: ${e.message}", e)
            }
        }
    }

    private fun buildRsaPublicKey(nB64: String, eB64: String): PublicKey {
        val nBytes = Base64.getUrlDecoder().decode(padBase64Url(nB64))
        val eBytes = Base64.getUrlDecoder().decode(padBase64Url(eB64))
        val n = BigInteger(1, nBytes)
        val e = BigInteger(1, eBytes)
        val spec = RSAPublicKeySpec(n, e)
        return KeyFactory.getInstance("RSA").generatePublic(spec)
    }

    private fun padBase64Url(s: String): String {
        val rem = s.length % 4
        return if (rem == 0) s else s + "=".repeat(4 - rem)
    }
}
