package com.bffgin.backend.test_support;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.math.BigInteger;
import java.nio.charset.StandardCharsets;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.NoSuchAlgorithmException;
import java.security.Signature;
import java.security.interfaces.RSAPrivateKey;
import java.security.interfaces.RSAPublicKey;
import java.time.Instant;
import java.util.Base64;

/**
 * テスト専用のJWT生成ヘルパー(backend-rustのjwt.rs内make_hmac_token・
 * backend-c/backend-cppのtest_token_helperと同じ役割)。
 * 本番コード(HmacVerifier/JwksVerifier)は署名の検証しか行わないため、
 * 有効な署名付きトークンをテスト側で自作する必要がある
 */
public final class TestTokenHelper {

    private static final ObjectMapper MAPPER = new ObjectMapper();

    private TestTokenHelper() {
    }

    public static String makeHmacToken(String secret, String iss, String aud, String sub, long expOffsetSecs) {
        return makeHmacToken(secret, iss, aud, sub, expOffsetSecs, "HS256");
    }

    public static String makeHmacToken(String secret, String iss, String aud, String sub, long expOffsetSecs,
            String alg) {
        return makeHmacToken(secret, iss, aud, sub, expOffsetSecs, alg, null);
    }

    /** azpを指定できる版(外部公開APIのRequireExternalClientAuthテスト用) */
    public static String makeHmacToken(String secret, String iss, String aud, String sub, long expOffsetSecs,
            String alg, String azp) {
        String header = b64url(headerJson(alg));
        String payload = b64url(payloadJson(iss, aud, sub, expOffsetSecs, azp));
        String signingInput = header + "." + payload;
        String signature = b64url(hmacSha256(secret, signingInput));
        return signingInput + "." + signature;
    }

    public static KeyPair generateRsaKeyPair() {
        try {
            KeyPairGenerator gen = KeyPairGenerator.getInstance("RSA");
            gen.initialize(2048);
            return gen.generateKeyPair();
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException(e);
        }
    }

    public static String makeRsaToken(RSAPrivateKey privateKey, String kid, String iss, String aud, String sub,
            long expOffsetSecs) {
        return makeRsaToken(privateKey, kid, iss, aud, sub, expOffsetSecs, "RS256");
    }

    public static String makeRsaToken(RSAPrivateKey privateKey, String kid, String iss, String aud, String sub,
            long expOffsetSecs, String alg) {
        return makeRsaToken(privateKey, kid, iss, aud, sub, expOffsetSecs, alg, null);
    }

    /** azpを指定できる版(外部公開APIのRequireExternalClientAuthテスト用) */
    public static String makeRsaToken(RSAPrivateKey privateKey, String kid, String iss, String aud, String sub,
            long expOffsetSecs, String alg, String azp) {
        ObjectNode header = MAPPER.createObjectNode();
        header.put("alg", alg);
        header.put("typ", "JWT");
        header.put("kid", kid);
        String headerB64 = b64url(toBytes(header));
        String payloadB64 = b64url(payloadJson(iss, aud, sub, expOffsetSecs, azp));
        String signingInput = headerB64 + "." + payloadB64;
        try {
            Signature signature = Signature.getInstance("SHA256withRSA");
            signature.initSign(privateKey);
            signature.update(signingInput.getBytes(StandardCharsets.UTF_8));
            String sig = b64url(signature.sign());
            return signingInput + "." + sig;
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    /** JWKSレスポンスのn/e(base64url、big-endian符号無し整数)を作る */
    public static String rsaModulusB64Url(RSAPublicKey key) {
        return b64urlBigInteger(key.getModulus());
    }

    public static String rsaExponentB64Url(RSAPublicKey key) {
        return b64urlBigInteger(key.getPublicExponent());
    }

    private static String b64urlBigInteger(BigInteger value) {
        byte[] bytes = value.toByteArray();
        // BigInteger#toByteArrayは符号ビットのための先頭ゼロバイトを付けることがあるため取り除く
        if (bytes.length > 1 && bytes[0] == 0) {
            byte[] trimmed = new byte[bytes.length - 1];
            System.arraycopy(bytes, 1, trimmed, 0, trimmed.length);
            bytes = trimmed;
        }
        return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
    }

    private static byte[] headerJson(String alg) {
        ObjectNode header = MAPPER.createObjectNode();
        header.put("alg", alg);
        header.put("typ", "JWT");
        return toBytes(header);
    }

    private static byte[] payloadJson(String iss, String aud, String sub, long expOffsetSecs) {
        return payloadJson(iss, aud, sub, expOffsetSecs, null);
    }

    private static byte[] payloadJson(String iss, String aud, String sub, long expOffsetSecs, String azp) {
        ObjectNode payload = MAPPER.createObjectNode();
        payload.put("sub", sub);
        payload.put("iss", iss);
        payload.put("aud", aud);
        payload.put("exp", Instant.now().getEpochSecond() + expOffsetSecs);
        if (azp != null) {
            payload.put("azp", azp);
        }
        return toBytes(payload);
    }

    private static byte[] toBytes(ObjectNode node) {
        try {
            return MAPPER.writeValueAsBytes(node);
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    private static String b64url(byte[] data) {
        return Base64.getUrlEncoder().withoutPadding().encodeToString(data);
    }

    private static byte[] hmacSha256(String secret, String data) {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            return mac.doFinal(data.getBytes(StandardCharsets.UTF_8));
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }
}
