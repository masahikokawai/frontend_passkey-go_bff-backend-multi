package com.bffgin.backend.auth;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import java.util.Base64;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * JWTの`iss`クレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
 * (backend(Go)のauthjwt.Dispatcher・backend-rust/backend-c/backend-cppと同じ2段構造)。
 * issの詐称は委譲先の署名検証で弾かれる: 「振り分けのために覗く」ことと
 * 「検証を信頼する」ことは別、という2段階構造を維持する
 */
public final class Dispatcher {

    public static final String LOCAL_HMAC_ISSUER = "bff-gin-local-hmac";
    public static final String LOCAL_RSA_ISSUER = "bff-gin-local-rsa";

    public static boolean isLocalIssuer(String iss) {
        return LOCAL_HMAC_ISSUER.equals(iss) || LOCAL_RSA_ISSUER.equals(iss);
    }

    private final Map<String, TokenVerifier> byIssuer = new ConcurrentHashMap<>();
    private final ObjectMapper mapper = new ObjectMapper();

    public Dispatcher register(String issuer, TokenVerifier verifier) {
        byIssuer.put(issuer, verifier);
        return this;
    }

    public Claims verify(String token) throws VerifyException {
        String[] parts = token.split("\\.");
        if (parts.length != 3) {
            throw new VerifyException("malformed JWT");
        }
        String iss;
        try {
            byte[] payload = Base64.getUrlDecoder().decode(padBase64Url(parts[1]));
            JsonNode json = mapper.readTree(payload);
            JsonNode issNode = json.get("iss");
            iss = issNode == null ? null : issNode.asText();
        } catch (Exception e) {
            throw new VerifyException("failed to peek iss claim", e);
        }

        if (iss == null) {
            throw new VerifyException("unknown issuer: null");
        }
        TokenVerifier verifier = byIssuer.get(iss);
        if (verifier == null) {
            throw new VerifyException("unknown issuer: " + iss);
        }
        return verifier.verify(token);
    }

    private static String padBase64Url(String s) {
        int rem = s.length() % 4;
        return rem == 0 ? s : s + "=".repeat(4 - rem);
    }
}
