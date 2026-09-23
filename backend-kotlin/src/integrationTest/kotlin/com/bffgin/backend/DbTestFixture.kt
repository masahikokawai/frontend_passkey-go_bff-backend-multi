package com.bffgin.backend

import com.zaxxer.hikari.HikariConfig
import com.zaxxer.hikari.HikariDataSource
import java.sql.SQLException
import java.sql.Statement

/**
 * 実DB(docker-compose上のMySQL)結合テスト共通のfixture。
 * テストごとに一意なemail/keycloak_subでユーザー・ラベルを作り、共有の開発用DBを汚さない
 * (backend-java/backend-cpp/backend-cのDbTestFixture/db_fixtureと同じ設計。過去にbackend-rustで
 * 固定fixture行の共有が原因のテスト間干渉が見つかった経緯があるため、必ずテストごとに一意な行を作る)。
 *
 * このクラス自体はテスト用の道具であり、本番のTaskRepositoryとは違い普通の同期JDBC呼び出しで
 * よい(結合テストのセットアップ/後始末はコルーチンディスパッチャの外、JUnit5のライフサイクル
 * メソッドから直接呼ばれるため)
 */
class DbTestFixture : AutoCloseable {

    val dataSource: HikariDataSource

    init {
        val config = testConfig()
        val hikariConfig = HikariConfig()
        hikariConfig.jdbcUrl = config.jdbcUrl
        hikariConfig.username = config.dbUser
        hikariConfig.password = config.dbPassword
        hikariConfig.maximumPoolSize = 5
        dataSource = HikariDataSource(hikariConfig)
    }

    companion object {
        fun testConfig(): Config = Config.fromEnv()
    }

    fun createUser(uniqueSuffix: String): Long {
        dataSource.connection.use { conn ->
            conn.prepareStatement(
                "INSERT INTO users (email, name, role, created_at, updated_at) VALUES (?, ?, 1, NOW(), NOW())",
                Statement.RETURN_GENERATED_KEYS,
            ).use { ps ->
                ps.setString(1, "backend-kotlin-test-$uniqueSuffix@example.com")
                ps.setString(2, "backend-kotlin-test-$uniqueSuffix")
                ps.executeUpdate()
                ps.generatedKeys.use { keys ->
                    keys.next()
                    return keys.getLong(1)
                }
            }
        }
    }

    fun createUserWithKeycloakSub(uniqueSuffix: String, keycloakSub: String): Long {
        val userId = createUser(uniqueSuffix)
        dataSource.connection.use { conn ->
            conn.prepareStatement(
                "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) VALUES (?, ?, NOW(), NOW())",
            ).use { ps ->
                ps.setLong(1, userId)
                ps.setString(2, keycloakSub)
                ps.executeUpdate()
            }
        }
        return userId
    }

    fun createLabel(uniqueSuffix: String): Long {
        dataSource.connection.use { conn ->
            conn.prepareStatement(
                "INSERT INTO labels (name, created_at, updated_at) VALUES (?, NOW(), NOW())",
                Statement.RETURN_GENERATED_KEYS,
            ).use { ps ->
                ps.setString(1, "backend-kotlin-test-label-$uniqueSuffix")
                ps.executeUpdate()
                ps.generatedKeys.use { keys ->
                    keys.next()
                    return keys.getLong(1)
                }
            }
        }
    }

    /** ベストエフォートの後始末(アサート前に実行、backend-java/backend-rust/backend-c/backend-cppと同じ方針) */
    fun cleanupUser(userId: Long) {
        try {
            dataSource.connection.use { conn ->
                conn.prepareStatement(
                    "DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE user_id = ?)",
                ).use { ps ->
                    ps.setLong(1, userId)
                    ps.executeUpdate()
                }
                conn.prepareStatement("DELETE FROM tasks WHERE user_id = ?").use { ps ->
                    ps.setLong(1, userId)
                    ps.executeUpdate()
                }
                conn.prepareStatement("DELETE FROM user_keycloaks WHERE user_id = ?").use { ps ->
                    ps.setLong(1, userId)
                    ps.executeUpdate()
                }
                conn.prepareStatement("DELETE FROM users WHERE id = ?").use { ps ->
                    ps.setLong(1, userId)
                    ps.executeUpdate()
                }
            }
        } catch (ignored: SQLException) {
            // ベストエフォート: 後始末の失敗でテスト結果自体を壊さない
        }
    }

    fun cleanupLabel(labelId: Long) {
        try {
            dataSource.connection.use { conn ->
                conn.prepareStatement("DELETE FROM labels WHERE id = ?").use { ps ->
                    ps.setLong(1, labelId)
                    ps.executeUpdate()
                }
            }
        } catch (ignored: SQLException) {
        }
    }

    override fun close() {
        dataSource.close()
    }
}
