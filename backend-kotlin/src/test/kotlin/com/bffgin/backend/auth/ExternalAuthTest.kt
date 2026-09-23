package com.bffgin.backend.auth

import com.bffgin.backend.rest.ExternalApiError
import com.bffgin.backend.test_support.MockJwksServer
import com.bffgin.backend.test_support.TestTokenHelper
import com.bffgin.backend.test_support.assertThrowsSuspend
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.security.interfaces.RSAPrivateKey
import java.security.interfaces.RSAPublicKey

/**
 * 外部公開API専用の認証(RequireExternalClientAuth相当)をDB無しで検証する。
 * Keycloak発行トークンはRSA/JWKS(モックJWKSサーバー)で代替する(backend-c/backend-cppの
 * external_handler_integration_testの認証系テストと同じシナリオ)
 */
class ExternalAuthTest {

    private val externalApiClientId = "external-api-client"
    private val keycloakIssuer = "https://keycloak.example/realms/training"
    private val hmacSecret = "test-secret-at-least-32-bytes-long!!"

    @Test
    fun acceptsKeycloakTokenWithMatchingAzp() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val dispatcher = Dispatcher().register(keycloakIssuer, JwksVerifier(mockJwks.jwksUrl(), keycloakIssuer, "backend"))

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", keycloakIssuer, "backend", "1", 3600,
                azp = externalApiClientId,
            )
            val claims = ExternalAuth.requireExternalClient("Bearer $token", dispatcher, externalApiClientId)
            assertEquals(externalApiClientId, claims.azp)
        }
    }

    @Test
    fun rejectsMissingAuthorizationHeader() = runTest {
        val dispatcher = Dispatcher()
        val e = assertThrowsSuspend<ExternalApiError> {
            ExternalAuth.requireExternalClient(null, dispatcher, externalApiClientId)
        }
        assertEquals(ExternalApiError.Kind.UNAUTHENTICATED, e.kind)
    }

    @Test
    fun rejectsTokenWithWrongAzp() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val dispatcher = Dispatcher().register(keycloakIssuer, JwksVerifier(mockJwks.jwksUrl(), keycloakIssuer, "backend"))

            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", keycloakIssuer, "backend", "1", 3600,
                azp = "some-other-client",
            )
            val e = assertThrowsSuspend<ExternalApiError> {
                ExternalAuth.requireExternalClient("Bearer $token", dispatcher, externalApiClientId)
            }
            assertEquals(ExternalApiError.Kind.UNAUTHENTICATED, e.kind)
        }
    }

    @Test
    fun rejectsTokenWithMissingAzp() = runTest {
        val keyPair = TestTokenHelper.generateRsaKeyPair()
        MockJwksServer().use { mockJwks ->
            mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)
            val dispatcher = Dispatcher().register(keycloakIssuer, JwksVerifier(mockJwks.jwksUrl(), keycloakIssuer, "backend"))

            // azpクレームを付けない(makeRsaTokenのazp省略時の既定)
            val token = TestTokenHelper.makeRsaToken(
                keyPair.private as RSAPrivateKey, "kid-1", keycloakIssuer, "backend", "1", 3600,
            )
            val e = assertThrowsSuspend<ExternalApiError> {
                ExternalAuth.requireExternalClient("Bearer $token", dispatcher, externalApiClientId)
            }
            assertEquals(ExternalApiError.Kind.UNAUTHENTICATED, e.kind)
        }
    }

    /**
     * 【重要な確認、間違えやすい観点】ローカルHMAC発行のトークンは、署名検証自体は
     * (正しい秘密鍵・iss・aud・azpであっても)正しく通るが、外部公開APIはKeycloak発行分のみを
     * 受け付けるため、issがローカルという理由だけで拒否されなければならない
     */
    @Test
    fun rejectsLocalHmacIssuerEvenWithCorrectAzp() = runTest {
        val dispatcher = Dispatcher()
            .register(Dispatcher.LOCAL_HMAC_ISSUER, HmacVerifier(hmacSecret, Dispatcher.LOCAL_HMAC_ISSUER, "backend"))

        val token = TestTokenHelper.makeHmacToken(
            hmacSecret, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "1", 3600,
            azp = externalApiClientId,
        )
        val e = assertThrowsSuspend<ExternalApiError> {
            ExternalAuth.requireExternalClient("Bearer $token", dispatcher, externalApiClientId)
        }
        assertEquals(ExternalApiError.Kind.UNAUTHENTICATED, e.kind)
    }
}
