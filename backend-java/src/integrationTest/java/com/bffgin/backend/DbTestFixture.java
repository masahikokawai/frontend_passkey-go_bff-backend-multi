package com.bffgin.backend;

import com.zaxxer.hikari.HikariConfig;
import com.zaxxer.hikari.HikariDataSource;

import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;

/**
 * 実DB(docker-compose上のMySQL)結合テスト共通のfixture。
 * テストごとに一意なemail/keycloak_subでユーザー・ラベルを作り、共有の開発用DBを汚さない
 * (backend-cpp/backend-cのtests/db_fixture.*・test_repository_integration_test.c/cppと同じ設計。
 * 過去にbackend-rustで固定fixture行の共有が原因のテスト間干渉が見つかった経緯があるため、
 * 必ずテストごとに一意な行を作る)
 */
public final class DbTestFixture implements AutoCloseable {

    public final HikariDataSource dataSource;

    public DbTestFixture() {
        Config config = testConfig();
        HikariConfig hikariConfig = new HikariConfig();
        hikariConfig.setJdbcUrl(config.jdbcUrl());
        hikariConfig.setUsername(config.dbUser());
        hikariConfig.setPassword(config.dbPassword());
        hikariConfig.setMaximumPoolSize(5);
        this.dataSource = new HikariDataSource(hikariConfig);
    }

    public static Config testConfig() {
        return Config.fromEnv();
    }

    public long createUser(String uniqueSuffix) throws SQLException {
        try (Connection conn = dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "INSERT INTO users (email, name, role, created_at, updated_at) VALUES (?, ?, 1, NOW(), NOW())",
                        Statement.RETURN_GENERATED_KEYS)) {
            ps.setString(1, "backend-java-test-" + uniqueSuffix + "@example.com");
            ps.setString(2, "backend-java-test-" + uniqueSuffix);
            ps.executeUpdate();
            try (ResultSet keys = ps.getGeneratedKeys()) {
                keys.next();
                return keys.getLong(1);
            }
        }
    }

    public long createUserWithKeycloakSub(String uniqueSuffix, String keycloakSub) throws SQLException {
        long userId = createUser(uniqueSuffix);
        try (Connection conn = dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) VALUES (?, ?, NOW(), NOW())")) {
            ps.setLong(1, userId);
            ps.setString(2, keycloakSub);
            ps.executeUpdate();
        }
        return userId;
    }

    public long createLabel(String uniqueSuffix) throws SQLException {
        try (Connection conn = dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "INSERT INTO labels (name, created_at, updated_at) VALUES (?, NOW(), NOW())",
                        Statement.RETURN_GENERATED_KEYS)) {
            ps.setString(1, "backend-java-test-label-" + uniqueSuffix);
            ps.executeUpdate();
            try (ResultSet keys = ps.getGeneratedKeys()) {
                keys.next();
                return keys.getLong(1);
            }
        }
    }

    /** ベストエフォートの後始末(アサート前に実行、backend-rust/backend-c/backend-cppと同じ方針) */
    public void cleanupUser(long userId) {
        try (Connection conn = dataSource.getConnection()) {
            try (PreparedStatement ps = conn.prepareStatement(
                    "DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE user_id = ?)")) {
                ps.setLong(1, userId);
                ps.executeUpdate();
            }
            try (PreparedStatement ps = conn.prepareStatement("DELETE FROM tasks WHERE user_id = ?")) {
                ps.setLong(1, userId);
                ps.executeUpdate();
            }
            try (PreparedStatement ps = conn.prepareStatement("DELETE FROM user_keycloaks WHERE user_id = ?")) {
                ps.setLong(1, userId);
                ps.executeUpdate();
            }
            try (PreparedStatement ps = conn.prepareStatement("DELETE FROM users WHERE id = ?")) {
                ps.setLong(1, userId);
                ps.executeUpdate();
            }
        } catch (SQLException ignored) {
            // ベストエフォート: 後始末の失敗でテスト結果自体を壊さない
        }
    }

    public void cleanupLabel(long labelId) {
        try (Connection conn = dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement("DELETE FROM labels WHERE id = ?")) {
            ps.setLong(1, labelId);
            ps.executeUpdate();
        } catch (SQLException ignored) {
        }
    }

    @Override
    public void close() {
        dataSource.close();
    }
}
