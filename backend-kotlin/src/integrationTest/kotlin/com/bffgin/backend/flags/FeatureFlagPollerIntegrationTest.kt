package com.bffgin.backend.flags

import com.bffgin.backend.DbTestFixture
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test

/**
 * 実DBに一意なテスト専用flag_keyの行を挿入し、pollOnce()直後にvariation()の
 * フォールバック規則を検証する(backend-cpp/backend-cのfeature_flag_poller_test相当)。
 * 他機能が参照する既存のflag行(backend.external-tasks-pagination-v2等)には一切触れない
 */
class FeatureFlagPollerIntegrationTest {

    private lateinit var fixture: DbTestFixture
    private lateinit var poller: FeatureFlagPoller
    private var flagKeys: MutableList<String> = mutableListOf()

    @BeforeEach
    fun setUp() {
        fixture = DbTestFixture()
        poller = FeatureFlagPoller(fixture.dataSource)
    }

    @AfterEach
    fun tearDown() {
        for (key in flagKeys) {
            fixture.dataSource.connection.use { conn ->
                conn.prepareStatement("DELETE FROM feature_flags WHERE flag_key = ?").use { ps ->
                    ps.setString(1, key)
                    ps.executeUpdate()
                }
            }
        }
        fixture.close()
    }

    private fun insertFlag(key: String, enabled: Boolean, defaultVariation: String) {
        flagKeys.add(key)
        fixture.dataSource.connection.use { conn ->
            conn.prepareStatement(
                "INSERT INTO feature_flags (flag_key, description, default_variation, enabled, created_at, updated_at) " +
                    "VALUES (?, ?, ?, ?, NOW(), NOW())",
            ).use { ps ->
                ps.setString(1, key)
                ps.setString(2, "backend-kotlin-test")
                ps.setString(3, defaultVariation)
                ps.setBoolean(4, enabled)
                ps.executeUpdate()
            }
        }
    }

    private fun uniqueKey(name: String) = "backend-kotlin-test-$name-${System.nanoTime()}"

    @Test
    fun variationReturnsDefaultVariationWhenEnabled() = runBlocking {
        val key = uniqueKey("enabled")
        insertFlag(key, enabled = true, defaultVariation = "on")
        poller.pollOnce()
        assertEquals("on", poller.variation(key, "off"))
    }

    @Test
    fun variationReturnsFallbackWhenDisabled() = runBlocking {
        val key = uniqueKey("disabled")
        insertFlag(key, enabled = false, defaultVariation = "on")
        poller.pollOnce()
        assertEquals("off", poller.variation(key, "off"))
    }

    @Test
    fun variationReturnsFallbackWhenNotFound() = runBlocking {
        val key = uniqueKey("missing")
        poller.pollOnce()
        assertEquals("off", poller.variation(key, "off"))
    }
}
