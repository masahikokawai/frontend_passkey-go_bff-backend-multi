package com.bffgin.backend.auth

import com.bffgin.backend.test_support.MockJwksServer
import com.bffgin.backend.test_support.TestTokenHelper
import com.bffgin.backend.test_support.assertThrowsSuspend
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.security.interfaces.RSAPrivateKey
import java.security.interfaces.RSAPublicKey

/**
 * 自プロセス内蔵のモックJWKSサーバーを使い、実際のRS256署名検証・kidキャッシュ・
 * 未知kid時の再取得ロジックを検証する(backend-java/backend-c/backend-cppのjwksテストと
 * 同じ設計)。DB・Keycloak・bff実プロセスは不要
 */
class JwksVerifierTest {

    @Test
    fun acceptsValidTokenSignedWithKnownKey() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val verifier = JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend")

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", "https://issuer.example", "backend", "99", 3600,
            )
            val claims = verifier.verify(token)
            assertEquals("99", claims.sub)
            assertEquals("https://issuer.example", claims.iss)
        }
    }

    @Test
    fun unknownKidTriggersRefreshThenSucceedsIfNowPresent() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            val verifier = JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend")
            // JwksVerifier生成時点ではまだ鍵ゼロ件 -> 最初の検証はkid不一致で1回だけ再取得を試みる
            mockJwks.addKey("kid-2", keyPair.public as RSAPublicKey)

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-2", "https://issuer.example", "backend", "1", 3600,
            )
            val claims = verifier.verify(token)
            assertEquals("1", claims.sub)
        }
    }

    @Test
    fun unknownKidStillUnknownAfterRefreshIsRejected() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-registered", keyPair.public as RSAPublicKey)
            val verifier = JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend")

            // JWKSには存在しないkidで署名したトークン -> 再取得しても見つからず拒否される
            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-does-not-exist", "https://issuer.example", "backend", "1", 3600,
            )
            assertThrowsSuspend<VerifyException> { verifier.verify(token) }
        }
    }

    @Test
    fun wrongIssuerIsRejectedEvenWithValidSignature() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val verifier = JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend")

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", "https://different-issuer.example", "backend", "1", 3600,
            )
            assertThrowsSuspend<VerifyException> { verifier.verify(token) }
        }
    }

    @Test
    fun wrongAudienceIsRejected() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val verifier = JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend")

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", "https://issuer.example", "someone-else", "1", 3600,
            )
            assertThrowsSuspend<VerifyException> { verifier.verify(token) }
        }
    }

    @Test
    fun expiredTokenIsRejected() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val verifier = JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend")

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", "https://issuer.example", "backend", "1", -3600,
            )
            assertThrowsSuspend<VerifyException> { verifier.verify(token) }
        }
    }

    /** アルゴリズム混同攻撃対策: ヘッダのalgがRS256以外なら拒否する */
    @Test
    fun rejectsUnexpectedAlgorithm() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val verifier = JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend")

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", "https://issuer.example", "backend", "1", 3600, "RS512",
            )
            assertThrowsSuspend<VerifyException> { verifier.verify(token) }
        }
    }
}
