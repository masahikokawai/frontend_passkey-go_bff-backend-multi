package com.bffgin.backend.auth;

import com.bffgin.backend.domain.TaskError;
import com.bffgin.backend.test_support.MockJwksServer;
import com.bffgin.backend.test_support.TestTokenHelper;
import org.junit.jupiter.api.Test;

import java.security.KeyPair;
import java.security.interfaces.RSAPrivateKey;
import java.security.interfaces.RSAPublicKey;

import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;
import static org.junit.jupiter.api.Assertions.assertThrows;

/**
 * 外部公開API(CONTRACT.mdセクション11)のRequireExternalClientAuthを検証する。
 * DB・実サーバーは不要(モックJWKSサーバーのみ、JwksVerifierTestと同じ設計)
 */
class ExternalAuthTest {

    private static final String EXTERNAL_CLIENT_ID = "external-api-client";

    @Test
    void acceptsKeycloakTokenWithMatchingAzp() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            Dispatcher dispatcher = new Dispatcher().register("https://keycloak.example",
                    new JwksVerifier(mockJwks.jwksUrl(), "https://keycloak.example", "backend"));

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://keycloak.example", "backend", "some-keycloak-sub", 3600, "RS256", EXTERNAL_CLIENT_ID);

            assertDoesNotThrow(() -> ExternalAuth.requireExternalClient(dispatcher, "Bearer " + token,
                    EXTERNAL_CLIENT_ID));
        }
    }

    @Test
    void rejectsMissingAuthorizationHeader() {
        Dispatcher dispatcher = new Dispatcher();
        TaskError e = assertThrows(TaskError.class,
                () -> ExternalAuth.requireExternalClient(dispatcher, null, EXTERNAL_CLIENT_ID));
        assertEqualsUnauthorized(e);
    }

    @Test
    void rejectsWrongAzp() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            Dispatcher dispatcher = new Dispatcher().register("https://keycloak.example",
                    new JwksVerifier(mockJwks.jwksUrl(), "https://keycloak.example", "backend"));

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://keycloak.example", "backend", "sub", 3600, "RS256", "some-other-client");

            TaskError e = assertThrows(TaskError.class, () -> ExternalAuth.requireExternalClient(dispatcher,
                    "Bearer " + token, EXTERNAL_CLIENT_ID));
            assertEqualsUnauthorized(e);
        }
    }

    @Test
    void rejectsMissingAzp() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            Dispatcher dispatcher = new Dispatcher().register("https://keycloak.example",
                    new JwksVerifier(mockJwks.jwksUrl(), "https://keycloak.example", "backend"));

            // azp無し(nullのまま)のトークン
            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://keycloak.example", "backend", "sub", 3600);

            TaskError e = assertThrows(TaskError.class, () -> ExternalAuth.requireExternalClient(dispatcher,
                    "Bearer " + token, EXTERNAL_CLIENT_ID));
            assertEqualsUnauthorized(e);
        }
    }

    /**
     * 最も見落としやすいケース: ローカルHMAC発行のトークンは、署名自体は正しく検証できても
     * (azpが偶然一致していても)、外部公開APIでは拒否されなければならない
     * (Client Credentials Grant、つまりKeycloak発行のみを受け付ける設計)
     */
    @Test
    void rejectsLocalHmacIssuerEvenWithCorrectAzp() {
        String secret = "test-secret-at-least-32-bytes-long!!";
        Dispatcher dispatcher = new Dispatcher().register(Dispatcher.LOCAL_HMAC_ISSUER,
                new HmacVerifier(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend"));

        String token = TestTokenHelper.makeHmacToken(secret, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "1",
                3600, "HS256", EXTERNAL_CLIENT_ID);

        TaskError e = assertThrows(TaskError.class,
                () -> ExternalAuth.requireExternalClient(dispatcher, "Bearer " + token, EXTERNAL_CLIENT_ID));
        assertEqualsUnauthorized(e);
    }

    @Test
    void rejectsLocalRsaIssuerEvenWithCorrectAzp() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            Dispatcher dispatcher = new Dispatcher().register(Dispatcher.LOCAL_RSA_ISSUER,
                    new JwksVerifier(mockJwks.jwksUrl(), Dispatcher.LOCAL_RSA_ISSUER, "backend"));

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    Dispatcher.LOCAL_RSA_ISSUER, "backend", "1", 3600, "RS256", EXTERNAL_CLIENT_ID);

            TaskError e = assertThrows(TaskError.class,
                    () -> ExternalAuth.requireExternalClient(dispatcher, "Bearer " + token, EXTERNAL_CLIENT_ID));
            assertEqualsUnauthorized(e);
        }
    }

    @Test
    void rejectsUnknownIssuer() {
        Dispatcher dispatcher = new Dispatcher();
        String token = TestTokenHelper.makeHmacToken("test-secret", "https://unknown-issuer.example", "backend", "1",
                3600, "HS256", EXTERNAL_CLIENT_ID);

        TaskError e = assertThrows(TaskError.class,
                () -> ExternalAuth.requireExternalClient(dispatcher, "Bearer " + token, EXTERNAL_CLIENT_ID));
        assertEqualsUnauthorized(e);
    }

    private static void assertEqualsUnauthorized(TaskError e) {
        org.junit.jupiter.api.Assertions.assertEquals(TaskError.Kind.UNAUTHORIZED, e.kind());
    }
}
