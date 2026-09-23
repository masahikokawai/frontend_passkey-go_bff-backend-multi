package com.bffgin.backend.test_support

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.databind.node.ObjectNode
import java.math.BigInteger
import java.nio.charset.StandardCharsets
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.Signature
import java.security.interfaces.RSAPrivateKey
import java.security.interfaces.RSAPublicKey
import java.time.Instant
import java.util.Base64
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * テスト専用のJWT生成ヘルパー(backend-java/backend-rust/backend-c/backend-cppの
 * test_token_helper相当)。本番コード(HmacVerifier/JwksVerifier)は署名の検証しか行わないため、
 * 有効な署名付きトークンをテスト側で自作する必要がある
 */
object TestTokenHelper {

    private val mapper = ObjectMapper()

    fun makeHmacToken(
        secret: String,
        iss: String,
        aud: String,
        sub: String,
        expOffsetSecs: Long,
        alg: String = "HS256",
        azp: String? = null,
    ): String {
        val header = b64url(headerJson(alg))
        val payload = b64url(payloadJson(iss, aud, sub, expOffsetSecs, azp))
        val signingInput = "$header.$payload"
        val signature = b64url(hmacSha256(secret, signingInput))
        return "$signingInput.$signature"
    }

    fun generateRsaKeyPair(): KeyPair {
        val gen = KeyPairGenerator.getInstance("RSA")
        gen.initialize(2048)
        return gen.generateKeyPair()
    }

    fun makeRsaToken(
        privateKey: RSAPrivateKey,
        kid: String,
        iss: String,
        aud: String,
        sub: String,
        expOffsetSecs: Long,
        alg: String = "RS256",
        azp: String? = null,
    ): String {
        val header = mapper.createObjectNode()
        header.put("alg", alg)
        header.put("typ", "JWT")
        header.put("kid", kid)
        val headerB64 = b64url(toBytes(header))
        val payloadB64 = b64url(payloadJson(iss, aud, sub, expOffsetSecs, azp))
        val signingInput = "$headerB64.$payloadB64"
        val signature = Signature.getInstance("SHA256withRSA")
        signature.initSign(privateKey)
        signature.update(signingInput.toByteArray(StandardCharsets.UTF_8))
        val sig = b64url(signature.sign())
        return "$signingInput.$sig"
    }

    /** JWKSレスポンスのn/e(base64url、big-endian符号無し整数)を作る */
    fun rsaModulusB64Url(key: RSAPublicKey): String = b64urlBigInteger(key.modulus)

    fun rsaExponentB64Url(key: RSAPublicKey): String = b64urlBigInteger(key.publicExponent)

    private fun b64urlBigInteger(value: BigInteger): String {
        var bytes = value.toByteArray()
        // BigInteger#toByteArrayは符号ビットのための先頭ゼロバイトを付けることがあるため取り除く
        if (bytes.size > 1 && bytes[0] == 0.toByte()) {
            bytes = bytes.copyOfRange(1, bytes.size)
        }
        return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
    }

    private fun headerJson(alg: String): ByteArray {
        val header = mapper.createObjectNode()
        header.put("alg", alg)
        header.put("typ", "JWT")
        return toBytes(header)
    }

    private fun payloadJson(iss: String, aud: String, sub: String, expOffsetSecs: Long, azp: String? = null): ByteArray {
        val payload = mapper.createObjectNode()
        payload.put("sub", sub)
        payload.put("iss", iss)
        payload.put("aud", aud)
        payload.put("exp", Instant.now().epochSecond + expOffsetSecs)
        if (azp != null) {
            payload.put("azp", azp)
        }
        return toBytes(payload)
    }

    private fun toBytes(node: ObjectNode): ByteArray = mapper.writeValueAsBytes(node)

    private fun b64url(data: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(data)

    private fun hmacSha256(secret: String, data: String): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(secret.toByteArray(StandardCharsets.UTF_8), "HmacSHA256"))
        return mac.doFinal(data.toByteArray(StandardCharsets.UTF_8))
    }
}
