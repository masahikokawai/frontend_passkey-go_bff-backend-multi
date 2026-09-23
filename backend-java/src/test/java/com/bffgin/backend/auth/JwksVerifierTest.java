package com.bffgin.backend.auth;

import com.bffgin.backend.test_support.MockJwksServer;
import com.bffgin.backend.test_support.TestTokenHelper;
import org.junit.jupiter.api.Test;

import java.security.KeyPair;
import java.security.interfaces.RSAPrivateKey;
import java.security.interfaces.RSAPublicKey;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

/**
 * 自プロセス内蔵のモックJWKSサーバーを使い、実際のRS256署名検証・kidキャッシュ・
 * 未知kid時の再取得ロジックを検証する(backend-c/tests/jwks_test.c・
 * backend-cppのjwks_test.cppと同じ設計)。DB・Keycloak・bff実プロセスは不要
 */
class JwksVerifierTest {

    @Test
    void acceptsValidTokenSignedWithKnownKey() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            JwksVerifier verifier = new JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend");

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://issuer.example", "backend", "99", 3600);
            Claims claims = verifier.verify(token);
            assertEquals("99", claims.sub());
            assertEquals("https://issuer.example", claims.iss());
        }
    }

    @Test
    void unknownKidTriggersRefreshThenSucceedsIfNowPresent() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            JwksVerifier verifier = new JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend");
            // JwksVerifier生成時点ではまだ鍵ゼロ件 -> 最初の検証はkid不一致で1回だけ再取得を試みる
            mockJwks.addKey("kid-2", (RSAPublicKey) keyPair.getPublic());

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-2",
                    "https://issuer.example", "backend", "1", 3600);
            Claims claims = verifier.verify(token);
            assertEquals("1", claims.sub());
        }
    }

    @Test
    void unknownKidStillUnknownAfterRefreshIsRejected() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-registered", (RSAPublicKey) keyPair.getPublic());
            JwksVerifier verifier = new JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend");

            // JWKSには存在しないkidで署名したトークン -> 再取得しても見つからず拒否される
            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-does-not-exist",
                    "https://issuer.example", "backend", "1", 3600);
            assertThrows(VerifyException.class, () -> verifier.verify(token));
        }
    }

    @Test
    void wrongIssuerIsRejectedEvenWithValidSignature() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            JwksVerifier verifier = new JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend");

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://different-issuer.example", "backend", "1", 3600);
            assertThrows(VerifyException.class, () -> verifier.verify(token));
        }
    }

    @Test
    void wrongAudienceIsRejected() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            JwksVerifier verifier = new JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend");

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://issuer.example", "someone-else", "1", 3600);
            assertThrows(VerifyException.class, () -> verifier.verify(token));
        }
    }

    @Test
    void expiredTokenIsRejected() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            JwksVerifier verifier = new JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend");

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://issuer.example", "backend", "1", -3600);
            assertThrows(VerifyException.class, () -> verifier.verify(token));
        }
    }

    /** アルゴリズム混同攻撃対策: ヘッダのalgがRS256以外なら拒否する */
    @Test
    void rejectsUnexpectedAlgorithm() throws Exception {
        KeyPair keyPair = TestTokenHelper.generateRsaKeyPair();
        try (MockJwksServer mockJwks = new MockJwksServer()) {
            mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());
            JwksVerifier verifier = new JwksVerifier(mockJwks.jwksUrl(), "https://issuer.example", "backend");

            String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1",
                    "https://issuer.example", "backend", "1", 3600, "RS512");
            assertThrows(VerifyException.class, () -> verifier.verify(token));
        }
    }
}
