package com.bffgin.backend.rest

import com.bffgin.backend.DbTestFixture
import com.bffgin.backend.auth.Dispatcher
import com.bffgin.backend.domain.TaskInput
import com.bffgin.backend.domain.TaskStatus
import com.bffgin.backend.flags.FeatureFlagPoller
import com.bffgin.backend.repository.TaskRepository
import com.bffgin.backend.test_support.MockJwksServer
import com.bffgin.backend.test_support.TestTokenHelper
import com.fasterxml.jackson.databind.ObjectMapper
import io.ktor.server.cio.CIO
import io.ktor.server.engine.ApplicationEngine
import io.ktor.server.engine.embeddedServer
import io.ktor.server.routing.routing
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.security.interfaces.RSAPrivateKey
import java.security.interfaces.RSAPublicKey
import java.time.LocalDate

/**
 * 実DB・モックJWKSサーバー(Keycloak相当として扱う)・外部公開API自身のKtorリスナー
 * (テスト用ポート)の3つを1プロセス内に立て、実際にHTTP GETを送ってエンドツーエンドに
 * 検証する(backend-c/backend-cpp/backend-javaのexternal_handler_integration_test相当)。
 * backend.external-tasks-pagination-v2は退避→書き換え→テスト後に復元する
 * (全言語で共有する1つのFeature Flagのため、他のテスト・実機動作に影響を残さない)
 */
class ExternalHandlerIntegrationTest {

    private val externalApiClientId = "external-api-client"
    private val keycloakIssuer = "https://keycloak.example/realms/training"
    private val flagKey = "backend.external-tasks-pagination-v2"

    private lateinit var fixture: DbTestFixture
    private lateinit var repository: TaskRepository
    private lateinit var mockJwks: MockJwksServer
    private lateinit var flagPoller: FeatureFlagPoller
    private lateinit var scope: CoroutineScope
    private lateinit var server: ApplicationEngine
    private var port: Int = 0
    private lateinit var keyPair: java.security.KeyPair
    private var userId: Long = 0
    private var originalFlagEnabled: Boolean = false
    private var originalFlagDefault: String = "off"

    private val http = HttpClient.newHttpClient()
    private val mapper = ObjectMapper()

    @BeforeEach
    fun setUp() {
        fixture = DbTestFixture()
        repository = TaskRepository(fixture.dataSource)
        userId = fixture.createUser("${System.nanoTime()}")

        mockJwks = MockJwksServer()
        keyPair = TestTokenHelper.generateRsaKeyPair()
        mockJwks.addKey("kid-1", keyPair.public as RSAPublicKey)

        val dispatcher = Dispatcher().register(
            keycloakIssuer,
            com.bffgin.backend.auth.JwksVerifier(mockJwks.jwksUrl(), keycloakIssuer, "backend"),
        )

        scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        flagPoller = FeatureFlagPoller(fixture.dataSource)
        flagPoller.start(scope)

        // 実行中の値を退避(テスト後に復元する)
        fixture.dataSource.connection.use { conn ->
            conn.prepareStatement("SELECT enabled, default_variation FROM feature_flags WHERE flag_key = ?").use { ps ->
                ps.setString(1, flagKey)
                ps.executeQuery().use { rs ->
                    if (rs.next()) {
                        originalFlagEnabled = rs.getBoolean("enabled")
                        originalFlagDefault = rs.getString("default_variation")
                    }
                }
            }
        }

        port = findFreePort()
        server = embeddedServer(CIO, port = port) {
            routing {
                externalRoutes(repository, dispatcher, flagPoller, externalApiClientId)
            }
        }
        server.start(wait = false)
        // サーバー起動を待つ(即座にリクエストすると接続拒否になりうる)
        Thread.sleep(300)
        runBlocking { flagPoller.pollOnce() }
    }

    @AfterEach
    fun tearDown() {
        try {
            restoreFlag()
        } finally {
            server.stop(0, 0)
            scope.cancel()
            mockJwks.close()
            fixture.cleanupUser(userId)
            fixture.close()
        }
    }

    private fun findFreePort(): Int {
        java.net.ServerSocket(0).use { return it.localPort }
    }

    private fun setFlag(enabled: Boolean, defaultVariation: String) {
        fixture.dataSource.connection.use { conn ->
            conn.prepareStatement("UPDATE feature_flags SET enabled = ?, default_variation = ? WHERE flag_key = ?").use { ps ->
                ps.setBoolean(1, enabled)
                ps.setString(2, defaultVariation)
                ps.setString(3, flagKey)
                ps.executeUpdate()
            }
        }
    }

    private fun restoreFlag() {
        setFlag(originalFlagEnabled, originalFlagDefault)
    }

    private fun validToken(): String = TestTokenHelper.makeRsaToken(
        keyPair.private as RSAPrivateKey, "kid-1", keycloakIssuer, "backend", "1", 3600,
        azp = externalApiClientId,
    )

    private fun get(path: String, token: String? = null): HttpResponse<String> {
        val builder = HttpRequest.newBuilder(URI.create("http://127.0.0.1:$port$path")).GET()
        if (token != null) {
            builder.header("Authorization", "Bearer $token")
        }
        return http.send(builder.build(), HttpResponse.BodyHandlers.ofString())
    }

    @Test
    fun rejectsMissingAuthorization() {
        val response = get("/external/v1/tasks?user_id=$userId")
        assertEquals(401, response.statusCode())
        assertEquals("unauthenticated", mapper.readTree(response.body()).get("error").asText())
    }

    @Test
    fun rejectsWrongAzp() {
        val token = TestTokenHelper.makeRsaToken(
            keyPair.private as RSAPrivateKey, "kid-1", keycloakIssuer, "backend", "1", 3600,
            azp = "wrong-client",
        )
        val response = get("/external/v1/tasks?user_id=$userId", token)
        assertEquals(401, response.statusCode())
    }

    @Test
    fun rejectsMissingUserId() {
        val response = get("/external/v1/tasks", validToken())
        assertEquals(400, response.statusCode())
        assertEquals("user_id_required", mapper.readTree(response.body()).get("error").asText())
    }

    @Test
    fun offsetPaginationAcrossPages() = runBlocking {
        val labelId = fixture.createLabel("${System.nanoTime()}")
        try {
            repository.create(userId, TaskInput("t1", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelId)), TaskStatus.WAITING)
            repository.create(userId, TaskInput("t2", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelId)), TaskStatus.WAITING)
            repository.create(userId, TaskInput("t3", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelId)), TaskStatus.WAITING)

            setFlag(false, "off")

            val page1 = get("/external/v1/tasks?user_id=$userId&page=1&page_size=2", validToken())
            assertEquals(200, page1.statusCode())
            val body1 = mapper.readTree(page1.body())
            assertEquals(2, body1.get("tasks").size())
            assertEquals(3, body1.get("total").asInt())
            assertEquals(1, body1.get("page").asInt())
            assertEquals(2, body1.get("page_size").asInt())

            val page2 = get("/external/v1/tasks?user_id=$userId&page=2&page_size=2", validToken())
            val body2 = mapper.readTree(page2.body())
            assertEquals(1, body2.get("tasks").size())
        } finally {
            fixture.cleanupLabel(labelId)
        }
    }

    @Test
    fun cursorPaginationChainsToNull() = runBlocking {
        val labelId = fixture.createLabel("${System.nanoTime()}")
        try {
            val id1 = repository.create(userId, TaskInput("c1", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelId)), TaskStatus.WAITING)
            val id2 = repository.create(userId, TaskInput("c2", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelId)), TaskStatus.WAITING)

            setFlag(true, "on")
            runBlocking { flagPoller.pollOnce() }

            val page1 = get("/external/v1/tasks?user_id=$userId&limit=1", validToken())
            assertEquals(200, page1.statusCode())
            val body1 = mapper.readTree(page1.body())
            assertEquals(1, body1.get("tasks").size())
            assertEquals(id1.toString(), body1.get("next_cursor").asText())

            val page2 = get("/external/v1/tasks?user_id=$userId&cursor=$id1&limit=1", validToken())
            val body2 = mapper.readTree(page2.body())
            assertEquals(1, body2.get("tasks").size())
            assertEquals(id2.toString(), body2.get("next_cursor").asText())

            val page3 = get("/external/v1/tasks?user_id=$userId&cursor=$id2&limit=1", validToken())
            val body3 = mapper.readTree(page3.body())
            assertEquals(0, body3.get("tasks").size())
            assertTrue(body3.get("next_cursor").isNull)
        } finally {
            fixture.cleanupLabel(labelId)
        }
    }
}
