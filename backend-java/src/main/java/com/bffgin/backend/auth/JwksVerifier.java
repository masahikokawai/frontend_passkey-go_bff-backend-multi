package com.bffgin.backend.auth;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.jsonwebtoken.JwtException;
import io.jsonwebtoken.Jwts;

import java.math.BigInteger;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.security.KeyFactory;
import java.security.NoSuchAlgorithmException;
import java.security.PublicKey;
import java.security.spec.InvalidKeySpecException;
import java.security.spec.RSAPublicKeySpec;
import java.time.Duration;
import java.util.Base64;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Keycloak発行・ローカルRSA発行(iss=Dispatcher.LOCAL_RSA_ISSUER)共通のJWKSベース検証。RS256。
 * kid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけJWKSを再取得する
 * (backend(Go)のjwks.go・backend-rust/backend-c/backend-cppと同じ「kid不一致時のみ再取得」戦略)
 */
public final class JwksVerifier implements TokenVerifier {

    private static final Logger log = LoggerFactory.getLogger(JwksVerifier.class);

    private final String jwksUrl;
    private final String issuer;
    private final String audience;
    private final HttpClient http;
    private final ObjectMapper mapper = new ObjectMapper();
    private volatile Map<String, PublicKey> keys = Map.of();

    public JwksVerifier(String jwksUrl, String issuer, String audience) {
        this.jwksUrl = jwksUrl;
        this.issuer = issuer;
        this.audience = audience;
        this.http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(5)).build();
    }

    @Override
    public Claims verify(String token) throws VerifyException {
        String[] parts = token.split("\\.");
        if (parts.length != 3) {
            throw new VerifyException("malformed token");
        }
        String kid = extractKid(parts[0]);
        if (kid == null) {
            throw new VerifyException("JWT header has no kid");
        }

        PublicKey key = keys.get(kid);
        if (key == null) {
            refresh();
            key = keys.get(kid);
            if (key == null) {
                throw new VerifyException("no public key found for kid=" + kid);
            }
        }

        try {
            var jws = Jwts.parser()
                    .verifyWith(key)
                    .build()
                    .parseSignedClaims(token);

            String alg = jws.getHeader().getAlgorithm();
            if (!"RS256".equals(alg)) {
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
            throw new VerifyException("RSA/JWKS verify failed: " + e.getMessage(), e);
        }
    }

    private String extractKid(String headerB64) throws VerifyException {
        try {
            byte[] raw = Base64.getUrlDecoder().decode(padBase64Url(headerB64));
            JsonNode header = mapper.readTree(raw);
            JsonNode kid = header.get("kid");
            return kid == null ? null : kid.asText();
        } catch (Exception e) {
            throw new VerifyException("failed to parse JWT header", e);
        }
    }

    private static String padBase64Url(String s) {
        int rem = s.length() % 4;
        if (rem == 0) {
            return s;
        }
        return s + "=".repeat(4 - rem);
    }

    private synchronized void refresh() throws VerifyException {
        try {
            HttpRequest request = HttpRequest.newBuilder(URI.create(jwksUrl))
                    .timeout(Duration.ofSeconds(5))
                    .GET()
                    .build();
            HttpResponse<String> response = http.send(request, HttpResponse.BodyHandlers.ofString());
            if (response.statusCode() != 200) {
                throw new VerifyException("JWKS fetch failed: HTTP " + response.statusCode());
            }
            JsonNode root = mapper.readTree(response.body());
            Map<String, PublicKey> newKeys = new ConcurrentHashMap<>();
            for (JsonNode k : root.withArray("keys")) {
                String kty = k.path("kty").asText("");
                String use = k.path("use").asText("");
                if (!"RSA".equals(kty) || (!use.isEmpty() && !"sig".equals(use))) {
                    continue;
                }
                String kid = k.path("kid").asText("");
                String n = k.path("n").asText("");
                String e = k.path("e").asText("");
                if (kid.isEmpty() || n.isEmpty() || e.isEmpty()) {
                    continue;
                }
                try {
                    newKeys.put(kid, buildRsaPublicKey(n, e));
                } catch (InvalidKeySpecException | NoSuchAlgorithmException ex) {
                    // 【バッド/グッドプラクティス】特定の鍵1件の構築に失敗しただけで
                    // JWKS取得全体を失敗させると、他の正常な鍵まで使えなくなってしまう
                    // (1つの壊れたエントリが全体を巻き込む)。ここでは該当エントリだけ
                    // スキップし、他の鍵は正常にキャッシュへ反映する
                    continue;
                }
            }
            keys = newKeys;
            log.debug("JWKS refreshed: url={} issuer={} keys={}", jwksUrl, issuer, newKeys.keySet());
        } catch (java.io.IOException | InterruptedException e) {
            throw new VerifyException("JWKS fetch failed: " + e.getMessage(), e);
        }
    }

    private static PublicKey buildRsaPublicKey(String nB64, String eB64)
            throws InvalidKeySpecException, NoSuchAlgorithmException {
        byte[] nBytes = Base64.getUrlDecoder().decode(padBase64Url(nB64));
        byte[] eBytes = Base64.getUrlDecoder().decode(padBase64Url(eB64));
        BigInteger n = new BigInteger(1, nBytes);
        BigInteger e = new BigInteger(1, eBytes);
        RSAPublicKeySpec spec = new RSAPublicKeySpec(n, e);
        return KeyFactory.getInstance("RSA").generatePublic(spec);
    }
}
