package com.bffgin.backend.test_support

import com.fasterxml.jackson.databind.ObjectMapper
import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.security.interfaces.RSAPublicKey
import java.util.concurrent.Executors

/**
 * テスト専用の自プロセス内蔵JWKSサーバー(backend-java/backend-c/backend-cppのモックJWKSサーバーと
 * 同じ役割)。実際にKeycloak/bffを起動せずにJwksVerifierのRS256検証・kidキャッシュ・
 * 未知kid時の再取得ロジックを検証できる。テスト専用の道具としてJDK標準のHttpServerを使う
 * (本番のREST実装はKtorだが、これはあくまでテスト用の使い捨てモックであり、
 * 「本番はKtor、Verifierが叩く先はただのHTTPサーバーであれば何でもよい」という点を示す)
 */
class MockJwksServer : AutoCloseable {

    private val server: HttpServer = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
    private val keys = mutableListOf<RSAPublicKey>()
    private val kids = mutableListOf<String>()
    private val mapper = ObjectMapper()

    init {
        server.createContext("/jwks") { exchange ->
            val body = buildJwksJson().toByteArray(StandardCharsets.UTF_8)
            exchange.responseHeaders.add("Content-Type", "application/json")
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.executor = Executors.newVirtualThreadPerTaskExecutor()
        server.start()
    }

    fun addKey(kid: String, key: RSAPublicKey) {
        kids.add(kid)
        keys.add(key)
    }

    fun jwksUrl(): String = "http://127.0.0.1:${server.address.port}/jwks"

    private fun buildJwksJson(): String {
        val root = mapper.createObjectNode()
        val keysNode = root.putArray("keys")
        for (i in keys.indices) {
            val key = keys[i]
            val k = mapper.createObjectNode()
            k.put("kty", "RSA")
            k.put("kid", kids[i])
            k.put("use", "sig")
            k.put("n", TestTokenHelper.rsaModulusB64Url(key))
            k.put("e", TestTokenHelper.rsaExponentB64Url(key))
            keysNode.add(k)
        }
        return root.toString()
    }

    override fun close() {
        server.stop(0)
    }
}
