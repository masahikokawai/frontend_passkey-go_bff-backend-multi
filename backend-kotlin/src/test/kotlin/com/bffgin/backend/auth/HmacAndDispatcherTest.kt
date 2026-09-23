package com.bffgin.backend.auth

import com.bffgin.backend.test_support.TestTokenHelper
import com.bffgin.backend.test_support.assertThrowsSuspend
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * backend-java/HmacAndDispatcherTest・backend-rustのsrc/auth/jwt.rs(mod tests)相当。
 * HmacVerifier単体とDispatcherの振り分けロジックを検証する(DB不要)
 */
class HmacAndDispatcherTest {

    // HS256はKeys.hmacShaKeyFor()で256bit(32バイト)以上の鍵長を要求するため、
    // 十分な長さのテスト用秘密鍵を使う
    private val secret = "test-secret-at-least-32-bytes-long!!"

    @Test
    fun isLocalIssuerMatchesHmacAndRsaOnly() {
        assertTrue(Dispatcher.isLocalIssuer(Dispatcher.LOCAL_HMAC_ISSUER))
        assertTrue(Dispatcher.isLocalIssuer(Dispatcher.LOCAL_RSA_ISSUER))
        assertTrue(!Dispatcher.isLocalIssuer("http://localhost:8082/realms/training"))
        assertTrue(!Dispatcher.isLocalIssuer(""))
    }

    @Test
    fun hmacVerifierAcceptsValidToken() = runTest {
        val token = TestTokenHelper.makeHmacToken(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", 3600)
        val verifier = HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend")
        val claims = verifier.verify(token)
        assertEquals("42", claims.sub)
        assertEquals(Dispatcher.LOCAL_HMAC_ISSUER, claims.iss)
    }

    @Test
    fun hmacVerifierRejectsWrongSecret() = runTest {
        val token = TestTokenHelper.makeHmacToken(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", 3600)
        val verifier = HmacVerifier("different-secret-also-32-bytes-long!", Dispatcher.LOCAL_HMAC_ISSUER, "backend")
        assertThrowsSuspend<VerifyException> { verifier.verify(token) }
    }

    @Test
    fun hmacVerifierRejectsExpiredToken() = runTest {
        val token = TestTokenHelper.makeHmacToken(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", -3600)
        val verifier = HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend")
        assertThrowsSuspend<VerifyException> { verifier.verify(token) }
    }

    @Test
    fun hmacVerifierRejectsWrongAudience() = runTest {
        val token = TestTokenHelper.makeHmacToken(secret, Dispatcher.LOCAL_HMAC_ISSUER, "someone-else", "42", 3600)
        val verifier = HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend")
        assertThrowsSuspend<VerifyException> { verifier.verify(token) }
    }

    @Test
    fun hmacVerifierRejectsWrongIssuer() = runTest {
        val token = TestTokenHelper.makeHmacToken(secret, "some-other-issuer", "backend", "42", 3600)
        val verifier = HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend")
        assertThrowsSuspend<VerifyException> { verifier.verify(token) }
    }

    /**
     * 【セキュリティ上重要な確認】ヘッダのalgが期待(HS256)と異なる場合は、
     * 仮に鍵が正しくても拒否されること(アルゴリズム混同攻撃対策)
     */
    @Test
    fun hmacVerifierRejectsUnexpectedAlgorithm() = runTest {
        val token = TestTokenHelper.makeHmacToken(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", 3600, "HS384")
        val verifier = HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend")
        assertThrowsSuspend<VerifyException> { verifier.verify(token) }
    }

    @Test
    fun hmacVerifierRejectsAlgNone() = runTest {
        // "alg":"none"は署名の要らない自己主張トークンになるため、必ず拒否されなければならない
        val header = java.util.Base64.getUrlEncoder().withoutPadding()
            .encodeToString("{\"alg\":\"none\",\"typ\":\"JWT\"}".toByteArray())
        val payload = java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(
            (
                "{\"sub\":\"42\",\"iss\":\"${Dispatcher.LOCAL_HMAC_ISSUER}\",\"aud\":\"backend\",\"exp\":" +
                    "${System.currentTimeMillis() / 1000 + 3600}}"
                ).toByteArray(),
        )
        val token = "$header.$payload."
        val verifier = HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend")
        assertThrowsSuspend<VerifyException> { verifier.verify(token) }
    }

    @Test
    fun dispatcherRoutesByIssuerAndRejectsUnknownIssuer() = runTest {
        val dispatcher = Dispatcher()
            .register(Dispatcher.LOCAL_HMAC_ISSUER, HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend"))

        val goodToken = TestTokenHelper.makeHmacToken(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "7", 3600)
        val claims = dispatcher.verify(goodToken)
        assertEquals("7", claims.sub)

        val unknownIssuerToken = TestTokenHelper.makeHmacToken(secret, "unknown-issuer", "backend", "7", 3600)
        assertThrowsSuspend<VerifyException> { dispatcher.verify(unknownIssuerToken) }
    }

    @Test
    fun dispatcherRejectsMalformedToken() = runTest {
        val dispatcher = Dispatcher()
            .register(Dispatcher.LOCAL_HMAC_ISSUER, HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend"))
        assertThrowsSuspend<VerifyException> { dispatcher.verify("not-a-jwt") }
    }
}
