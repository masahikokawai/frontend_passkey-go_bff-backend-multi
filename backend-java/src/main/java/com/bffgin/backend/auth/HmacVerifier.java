package com.bffgin.backend.auth;

import io.jsonwebtoken.Jwts;
import io.jsonwebtoken.JwtException;
import io.jsonwebtoken.security.Keys;

import javax.crypto.SecretKey;
import java.nio.charset.StandardCharsets;

/** ローカルHMAC発行(iss=Dispatcher.LOCAL_HMAC_ISSUER)の検証。HS256、共有シークレット */
public final class HmacVerifier implements TokenVerifier {

    private final SecretKey key;
    private final String issuer;
    private final String audience;

    public HmacVerifier(String secret, String issuer, String audience) {
        this.key = Keys.hmacShaKeyFor(secret.getBytes(StandardCharsets.UTF_8));
        this.issuer = issuer;
        this.audience = audience;
    }

    @Override
    public Claims verify(String token) throws VerifyException {
        try {
            var jws = Jwts.parser()
                    .verifyWith(key)
                    .build()
                    .parseSignedClaims(token);

            // 【セキュリティ上の確認、C/C++実装と同じ観点】jjwtはverifyWith()に渡した鍵の型
            // (SecretKey=HMAC系)と整合しないアルゴリズム(RS256等)のトークンをそもそも受理しないが、
            // ヘッダのalgが期待通りHS256であることも明示的に確認する(多層防御)
            String alg = jws.getHeader().getAlgorithm();
            if (!"HS256".equals(alg)) {
                throw new VerifyException("unexpected alg: " + alg);
            }

            io.jsonwebtoken.Claims claims = jws.getPayload();
            if (!issuer.equals(claims.getIssuer())) {
                throw new VerifyException("issuer mismatch");
            }
            if (!AudienceMatcher.matches(claims, audience)) {
                throw new VerifyException("audience mismatch");
            }
            String azp = claims.get("azp", String.class);
            return new Claims(claims.getSubject(), claims.getIssuer(), azp == null ? "" : azp);
        } catch (JwtException e) {
            throw new VerifyException("HMAC verify failed: " + e.getMessage(), e);
        }
    }
}
