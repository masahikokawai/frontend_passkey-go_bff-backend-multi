package com.bffgin.backend.auth;

import com.bffgin.backend.test_support.TestTokenHelper;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * backend-rustのsrc/auth/jwt.rs(mod tests)・backend-c/backend-cppのjwt_test相当。
 * HmacVerifier単体とDispatcherの振り分けロジックを検証する(DB不要)
 */
class HmacAndDispatcherTest {

    // HS256はKeys.hmacShaKeyFor()で256bit(32バイト)以上の鍵長を要求するため、
    // 十分な長さのテスト用秘密鍵を使う
    private static final String SECRET = "test-secret-at-least-32-bytes-long!!";

    @Test
    void isLocalIssuerMatchesHmacAndRsaOnly() {
        assertTrue(Dispatcher.isLocalIssuer(Dispatcher.LOCAL_HMAC_ISSUER));
        assertTrue(Dispatcher.isLocalIssuer(Dispatcher.LOCAL_RSA_ISSUER));
        assertTrue(!Dispatcher.isLocalIssuer("http://localhost:8082/realms/training"));
        assertTrue(!Dispatcher.isLocalIssuer(""));
    }

    @Test
    void hmacVerifierAcceptsValidToken() throws Exception {
        String token = TestTokenHelper.makeHmacToken(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", 3600);
        HmacVerifier verifier = new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend");
        Claims claims = verifier.verify(token);
        assertEquals("42", claims.sub());
        assertEquals(Dispatcher.LOCAL_HMAC_ISSUER, claims.iss());
    }

    @Test
    void hmacVerifierRejectsWrongSecret() {
        String token = TestTokenHelper.makeHmacToken(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", 3600);
        HmacVerifier verifier = new HmacVerifier("different-secret-also-32-bytes-long!", Dispatcher.LOCAL_HMAC_ISSUER, "backend");
        assertThrows(VerifyException.class, () -> verifier.verify(token));
    }

    @Test
    void hmacVerifierRejectsExpiredToken() {
        String token = TestTokenHelper.makeHmacToken(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", -3600);
        HmacVerifier verifier = new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend");
        assertThrows(VerifyException.class, () -> verifier.verify(token));
    }

    @Test
    void hmacVerifierRejectsWrongAudience() {
        String token = TestTokenHelper.makeHmacToken(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "someone-else", "42", 3600);
        HmacVerifier verifier = new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend");
        assertThrows(VerifyException.class, () -> verifier.verify(token));
    }

    @Test
    void hmacVerifierRejectsWrongIssuer() {
        String token = TestTokenHelper.makeHmacToken(SECRET, "some-other-issuer", "backend", "42", 3600);
        HmacVerifier verifier = new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend");
        assertThrows(VerifyException.class, () -> verifier.verify(token));
    }

    /**
     * 【セキュリティ上重要な確認】ヘッダのalgが期待(HS256)と異なる場合は、
     * 仮に鍵が正しくても拒否されること(アルゴリズム混同攻撃対策)
     */
    @Test
    void hmacVerifierRejectsUnexpectedAlgorithm() {
        String token = TestTokenHelper.makeHmacToken(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "42", 3600,
                "HS384");
        HmacVerifier verifier = new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend");
        assertThrows(VerifyException.class, () -> verifier.verify(token));
    }

    @Test
    void hmacVerifierRejectsAlgNone() {
        // "alg":"none"は署名の要らない自己主張トークンになるため、必ず拒否されなければならない
        String header = java.util.Base64.getUrlEncoder().withoutPadding()
                .encodeToString("{\"alg\":\"none\",\"typ\":\"JWT\"}".getBytes());
        String payload = java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(
                ("{\"sub\":\"42\",\"iss\":\"" + Dispatcher.LOCAL_HMAC_ISSUER + "\",\"aud\":\"backend\",\"exp\":"
                        + (System.currentTimeMillis() / 1000 + 3600) + "}").getBytes());
        String token = header + "." + payload + ".";
        HmacVerifier verifier = new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend");
        assertThrows(VerifyException.class, () -> verifier.verify(token));
    }

    @Test
    void dispatcherRoutesByIssuerAndRejectsUnknownIssuer() throws Exception {
        Dispatcher dispatcher = new Dispatcher()
                .register(Dispatcher.LOCAL_HMAC_ISSUER, new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend"));

        String goodToken = TestTokenHelper.makeHmacToken(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend", "7", 3600);
        Claims claims = dispatcher.verify(goodToken);
        assertEquals("7", claims.sub());

        String unknownIssuerToken = TestTokenHelper.makeHmacToken(SECRET, "unknown-issuer", "backend", "7", 3600);
        assertThrows(VerifyException.class, () -> dispatcher.verify(unknownIssuerToken));
    }

    @Test
    void dispatcherRejectsMalformedToken() {
        Dispatcher dispatcher = new Dispatcher()
                .register(Dispatcher.LOCAL_HMAC_ISSUER, new HmacVerifier(SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend"));
        assertThrows(VerifyException.class, () -> dispatcher.verify("not-a-jwt"));
    }
}
