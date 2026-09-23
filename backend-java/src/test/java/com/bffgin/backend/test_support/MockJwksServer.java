package com.bffgin.backend.test_support;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.security.interfaces.RSAPublicKey;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Executors;

/**
 * テスト専用の自プロセス内蔵JWKSサーバー(backend-c/tests/jwks_test.c・
 * backend-cpp/tests/mock_jwks_server.{hpp,cpp}と同じ役割)。
 * 実際にKeycloak/bffを起動せずにJwksVerifierのRS256検証・kidキャッシュ・
 * 未知kid時の再取得ロジックを検証できる
 */
public final class MockJwksServer implements AutoCloseable {

    private final HttpServer server;
    private final List<RSAPublicKey> keys = new ArrayList<>();
    private final List<String> kids = new ArrayList<>();
    private final ObjectMapper mapper = new ObjectMapper();

    public MockJwksServer() throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/jwks", exchange -> {
            byte[] body = buildJwksJson().getBytes(StandardCharsets.UTF_8);
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(200, body.length);
            try (var os = exchange.getResponseBody()) {
                os.write(body);
            }
        });
        server.setExecutor(Executors.newVirtualThreadPerTaskExecutor());
        server.start();
    }

    public void addKey(String kid, RSAPublicKey key) {
        kids.add(kid);
        keys.add(key);
    }

    public String jwksUrl() {
        return "http://127.0.0.1:" + server.getAddress().getPort() + "/jwks";
    }

    private String buildJwksJson() {
        ObjectNode root = mapper.createObjectNode();
        ArrayNode keysNode = root.putArray("keys");
        for (int i = 0; i < keys.size(); i++) {
            RSAPublicKey key = keys.get(i);
            ObjectNode k = mapper.createObjectNode();
            k.put("kty", "RSA");
            k.put("kid", kids.get(i));
            k.put("use", "sig");
            k.put("n", TestTokenHelper.rsaModulusB64Url(key));
            k.put("e", TestTokenHelper.rsaExponentB64Url(key));
            keysNode.add(k);
        }
        return root.toString();
    }

    @Override
    public void close() {
        server.stop(0);
    }
}
