package com.bffgin.backend.external;

import com.bffgin.backend.DbTestFixture;
import com.bffgin.backend.auth.Dispatcher;
import com.bffgin.backend.auth.HmacVerifier;
import com.bffgin.backend.auth.JwksVerifier;
import com.bffgin.backend.domain.TaskInput;
import com.bffgin.backend.domain.TaskStatus;
import com.bffgin.backend.flags.FeatureFlagPoller;
import com.bffgin.backend.repository.TaskRepository;
import com.bffgin.backend.test_support.MockJwksServer;
import com.bffgin.backend.test_support.TestTokenHelper;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.javalin.Javalin;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.security.KeyPair;
import java.security.interfaces.RSAPrivateKey;
import java.security.interfaces.RSAPublicKey;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.time.LocalDate;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * 実DB(docker-compose上のMySQL)+実HTTPサーバー+モックJWKSサーバーによる、外部公開API
 * (/external/v1/tasks)の結合テスト。TaskGrpcIntegrationTestと同じ「クラス全体で1つの
 * 実サーバーを起動する」設計。モックJWKSサーバーをKeycloak相当として扱う
 * (backend-c/backend-cppのexternal_handler_integration_testと同じ設計)
 */
class ExternalHandlerIntegrationTest {

    private static final String EXTERNAL_CLIENT_ID = "external-api-client";
    private static final String KEYCLOAK_ISSUER = "https://keycloak.example/realms/training";
    private static final String LOCAL_HMAC_SECRET = "test-secret-at-least-32-bytes-long!!";
    private static final int TEST_PORT = 18112;

    private static MockJwksServer mockJwks;
    private static KeyPair keyPair;
    private static FeatureFlagPoller flagPoller;
    private static Javalin app;
    private static DbTestFixture fixture;
    private static final HttpClient httpClient = HttpClient.newHttpClient();
    private static final ObjectMapper mapper = new ObjectMapper();

    private long userId;
    private String suffix;

    @BeforeAll
    static void startServer() throws Exception {
        fixture = new DbTestFixture();
        TaskRepository repository = new TaskRepository(fixture.dataSource);

        keyPair = TestTokenHelper.generateRsaKeyPair();
        mockJwks = new MockJwksServer();
        mockJwks.addKey("kid-1", (RSAPublicKey) keyPair.getPublic());

        Dispatcher dispatcher = new Dispatcher()
                .register(Dispatcher.LOCAL_HMAC_ISSUER,
                        new HmacVerifier(LOCAL_HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend"))
                .register(KEYCLOAK_ISSUER, new JwksVerifier(mockJwks.jwksUrl(), KEYCLOAK_ISSUER, "backend"));

        flagPoller = new FeatureFlagPoller(fixture.dataSource);
        flagPoller.pollOnce();

        ExternalHandler handler = new ExternalHandler(repository, dispatcher, flagPoller, EXTERNAL_CLIENT_ID);
        app = Javalin.create();
        app.get("/external/v1/tasks", handler.list);
        app.exception(com.bffgin.backend.domain.TaskError.class,
                (e, ctx) -> com.bffgin.backend.rest.RestErrorMapper.write(ctx, e));
        app.start(TEST_PORT);
    }

    @AfterAll
    static void stopServer() {
        app.stop();
        mockJwks.close();
        fixture.close();
    }

    @BeforeEach
    void setUp() throws Exception {
        suffix = System.nanoTime() + "-" + Math.abs(new java.util.Random().nextInt());
        userId = fixture.createUser(suffix);
    }

    @AfterEach
    void tearDown() {
        fixture.cleanupUser(userId);
    }

    private String keycloakToken() {
        return TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1", KEYCLOAK_ISSUER, "backend",
                "some-keycloak-sub-" + suffix, 3600, "RS256", EXTERNAL_CLIENT_ID);
    }

    private HttpResponse<String> get(String path, String bearer) throws Exception {
        HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + TEST_PORT + path)).GET();
        if (bearer != null) {
            builder.header("Authorization", "Bearer " + bearer);
        }
        return httpClient.send(builder.build(), HttpResponse.BodyHandlers.ofString());
    }

    @Test
    void rejectsMissingAuthorization() throws Exception {
        var resp = get("/external/v1/tasks?user_id=" + userId, null);
        assertEquals(401, resp.statusCode());
    }

    @Test
    void rejectsWrongAzp() throws Exception {
        String token = TestTokenHelper.makeRsaToken((RSAPrivateKey) keyPair.getPrivate(), "kid-1", KEYCLOAK_ISSUER,
                "backend", "sub", 3600, "RS256", "some-other-client");
        var resp = get("/external/v1/tasks?user_id=" + userId, token);
        assertEquals(401, resp.statusCode());
    }

    @Test
    void rejectsLocalHmacIssuerEvenWithCorrectAzp() throws Exception {
        String token = TestTokenHelper.makeHmacToken(LOCAL_HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend",
                String.valueOf(userId), 3600, "HS256", EXTERNAL_CLIENT_ID);
        var resp = get("/external/v1/tasks?user_id=" + userId, token);
        assertEquals(401, resp.statusCode());
    }

    @Test
    void rejectsMissingUserId() throws Exception {
        var resp = get("/external/v1/tasks", keycloakToken());
        assertEquals(400, resp.statusCode());
        JsonNode body = mapper.readTree(resp.body());
        assertEquals("user_id_required", body.get("error").asText());
    }

    @Test
    void rejectsInvalidUserId() throws Exception {
        var resp = get("/external/v1/tasks?user_id=not-a-number", keycloakToken());
        assertEquals(400, resp.statusCode());
        JsonNode body = mapper.readTree(resp.body());
        assertEquals("invalid_user_id", body.get("error").asText());
    }

    @Test
    void offsetPaginationAcrossPages() throws Exception {
        TaskRepository repository = new TaskRepository(fixture.dataSource);
        for (int i = 0; i < 3; i++) {
            repository.create(userId,
                    new TaskInput("task-" + i, "desc", "waiting", LocalDate.of(2099, 1, 1), List.of()),
                    TaskStatus.WAITING);
        }

        var page1 = get("/external/v1/tasks?user_id=" + userId + "&page=1&page_size=2", keycloakToken());
        assertEquals(200, page1.statusCode());
        JsonNode body1 = mapper.readTree(page1.body());
        assertEquals(2, body1.get("tasks").size());
        assertEquals(1, body1.get("page").asInt());
        assertEquals(2, body1.get("page_size").asInt());
        assertEquals(3, body1.get("total").asInt());

        var page2 = get("/external/v1/tasks?user_id=" + userId + "&page=2&page_size=2", keycloakToken());
        JsonNode body2 = mapper.readTree(page2.body());
        assertEquals(1, body2.get("tasks").size());
    }

    @Test
    void cursorPaginationChainsToNull() throws Exception {
        try {
            setFlag(true, "on");

            TaskRepository repository = new TaskRepository(fixture.dataSource);
            long id1 = repository.create(userId,
                    new TaskInput("cursor-task-1", "desc", "waiting", LocalDate.of(2099, 1, 1), List.of()),
                    TaskStatus.WAITING);
            long id2 = repository.create(userId,
                    new TaskInput("cursor-task-2", "desc", "waiting", LocalDate.of(2099, 1, 1), List.of()),
                    TaskStatus.WAITING);
            flagPoller.pollOnce();

            var page1 = get("/external/v1/tasks?user_id=" + userId + "&limit=1", keycloakToken());
            assertEquals(200, page1.statusCode());
            JsonNode body1 = mapper.readTree(page1.body());
            assertEquals(1, body1.get("tasks").size());
            assertEquals(String.valueOf(id1), body1.get("next_cursor").asText());

            var page2 = get("/external/v1/tasks?user_id=" + userId + "&cursor=" + id1 + "&limit=1", keycloakToken());
            JsonNode body2 = mapper.readTree(page2.body());
            assertEquals(1, body2.get("tasks").size());
            assertEquals(String.valueOf(id2), body2.get("next_cursor").asText());

            var page3 = get("/external/v1/tasks?user_id=" + userId + "&cursor=" + id2 + "&limit=1", keycloakToken());
            JsonNode body3 = mapper.readTree(page3.body());
            assertEquals(0, body3.get("tasks").size());
            assertTrue(body3.get("next_cursor").isNull());
        } finally {
            setFlag(false, "off");
            flagPoller.pollOnce();
        }
    }

    /**
     * backend.external-tasks-pagination-v2は全言語のテストスイートで共有されるフラグ行のため、
     * 変更前の状態を必ず復元する(finally節、他言語の結合テストと同じ方針)
     */
    private void setFlag(boolean enabled, String defaultVariation) throws Exception {
        try (Connection conn = fixture.dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "UPDATE feature_flags SET enabled = ?, default_variation = ? WHERE flag_key = 'backend.external-tasks-pagination-v2'")) {
            ps.setBoolean(1, enabled);
            ps.setString(2, defaultVariation);
            int updated = ps.executeUpdate();
            assertTrue(updated > 0, "backend.external-tasks-pagination-v2 flag row must already exist");
        }
    }
}
