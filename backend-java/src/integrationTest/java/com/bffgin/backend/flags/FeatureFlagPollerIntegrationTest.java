package com.bffgin.backend.flags;

import com.bffgin.backend.DbTestFixture;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.sql.Connection;
import java.sql.PreparedStatement;

import static org.junit.jupiter.api.Assertions.assertEquals;

/**
 * 実DB(docker-compose上のMySQL)に接続する結合テスト。
 * variation()のフォールバック規則を、実DBに一意なテスト専用flag_keyの行を挿入して検証する。
 * 他機能(外部公開API結合テスト等)が参照する既存のflag行(backend.external-tasks-pagination-v2)
 * には一切触れない(ExternalHandlerIntegrationTest参照、そちらは自分でその行を退避/復元する)
 */
class FeatureFlagPollerIntegrationTest {

    private DbTestFixture fixture;
    private String testFlagKey;

    @BeforeEach
    void setUp() {
        fixture = new DbTestFixture();
        testFlagKey = "backend-java-test-flag-" + System.nanoTime();
    }

    @AfterEach
    void tearDown() throws Exception {
        try (Connection conn = fixture.dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement("DELETE FROM feature_flags WHERE flag_key = ?")) {
            ps.setString(1, testFlagKey);
            ps.executeUpdate();
        } catch (Exception ignored) {
        }
        fixture.close();
    }

    @Test
    void variationReturnsDefaultVariationWhenEnabled() throws Exception {
        insertFlag(testFlagKey, true, "on");
        FeatureFlagPoller poller = new FeatureFlagPoller(fixture.dataSource);
        poller.pollOnce();
        assertEquals("on", poller.variation(testFlagKey, "off"));
    }

    @Test
    void variationReturnsFallbackWhenDisabled() throws Exception {
        insertFlag(testFlagKey, false, "on");
        FeatureFlagPoller poller = new FeatureFlagPoller(fixture.dataSource);
        poller.pollOnce();
        assertEquals("off", poller.variation(testFlagKey, "off"));
    }

    @Test
    void variationReturnsFallbackWhenNotFound() throws Exception {
        FeatureFlagPoller poller = new FeatureFlagPoller(fixture.dataSource);
        poller.pollOnce();
        assertEquals("off", poller.variation(testFlagKey, "off"));
    }

    private void insertFlag(String flagKey, boolean enabled, String defaultVariation) throws Exception {
        try (Connection conn = fixture.dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "INSERT INTO feature_flags (flag_key, description, enabled, default_variation, variations, created_at, updated_at) "
                                + "VALUES (?, ?, ?, ?, JSON_ARRAY(), NOW(), NOW())")) {
            ps.setString(1, flagKey);
            ps.setString(2, "backend-java integration test flag");
            ps.setBoolean(3, enabled);
            ps.setString(4, defaultVariation);
            ps.executeUpdate();
        }
    }
}
