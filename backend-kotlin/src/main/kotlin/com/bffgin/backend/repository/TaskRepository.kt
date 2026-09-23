package com.bffgin.backend.repository

import com.bffgin.backend.domain.Label
import com.bffgin.backend.domain.Task
import com.bffgin.backend.domain.TaskInput
import com.bffgin.backend.domain.TaskStatus
import com.bffgin.backend.domain.User
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.sql.Connection
import java.sql.PreparedStatement
import java.sql.ResultSet
import java.sql.Statement
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneOffset
import javax.sql.DataSource

/**
 * 生JDBC + PreparedStatementのみ(ORM禁止方針、他言語と統一)。
 * コネクションプールはHikariCPが担う(このクラス自体はDataSourceを受け取るだけ)。
 *
 * 【このKotlin実装で最も学習価値の高い設計判断、backend-java/Main.javaのstartRestServer()の
 * Javadocと対になるコメント】このクラスは「並行処理の安全性を型システムと明示的な
 * ディスパッチャ選択(withContext(Dispatchers.IO))で保証する」設計を採っている。
 * JDBCのようなブロッキング呼び出しを行う箇所は、呼び出し側(=このクラス自身)が
 * 「これはブロッキングI/Oである」と自己申告してDispatchers.IOへ明示的に切り替える必要があり、
 * それを怠ると同期呼び出しがコルーチンのデフォルトディスパッチャ(限られたスレッド数)を
 * 専有し、他の無関係なコルーチン全体が詰まってしまう。
 *
 * これは、同じくJVM上に実装済みのJava実装(backend-java、Virtual Threads)が
 * 「並行処理の安全性を自動化する」設計(JVMがI/Oブロックを検知して自動的にキャリアスレッドを
 * 解放するため、TaskRepository相当のクラスのJDBC呼び出し側には特別な記述が一切不要)を
 * 採っているのと意図的に対照的である(backend-java/src/main/java/com/bffgin/backend/Main.java・
 * README.md「アーキテクチャ選定」節参照)
 */
class TaskRepository(private val dataSource: DataSource) {

    data class OffsetPage(val tasks: List<Task>, val total: Long)

    /** v1(REST)向け: offsetページング */
    suspend fun listOffset(userId: Long, limit: Int, offset: Int): OffsetPage = withContext(Dispatchers.IO) {
        dataSource.connection.use { conn ->
            val total: Long
            conn.prepareStatement("SELECT COUNT(*) FROM tasks WHERE user_id = ?").use { ps ->
                ps.setLong(1, userId)
                ps.executeQuery().use { rs ->
                    rs.next()
                    total = rs.getLong(1)
                }
            }

            val tasks = mutableListOf<Task>()
            conn.prepareStatement(
                "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at " +
                    "FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
            ).use { ps ->
                ps.setLong(1, userId)
                ps.setInt(2, limit)
                ps.setInt(3, offset)
                ps.executeQuery().use { rs ->
                    while (rs.next()) {
                        tasks.add(rowToTask(rs))
                    }
                }
            }
            val withLabels = attachLabels(conn, tasks)
            OffsetPage(withLabels, total)
        }
    }

    /**
     * v2(gRPC)向け: id昇順のkeyset(cursor)ページング。
     * cursorの実体はuint64(直前ページ最後のtask.id、0=先頭)、合成キーは使わない(CONTRACT.mdセクション5)
     */
    suspend fun listCursor(userId: Long, afterId: Long, limit: Int): List<Task> = withContext(Dispatchers.IO) {
        dataSource.connection.use { conn ->
            val sql = "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at " +
                "FROM tasks WHERE user_id = ?" +
                (if (afterId > 0) " AND id > ?" else "") +
                " ORDER BY id ASC LIMIT ?"
            val tasks = mutableListOf<Task>()
            conn.prepareStatement(sql).use { ps ->
                var idx = 1
                ps.setLong(idx++, userId)
                if (afterId > 0) {
                    ps.setLong(idx++, afterId)
                }
                ps.setInt(idx, limit)
                ps.executeQuery().use { rs ->
                    while (rs.next()) {
                        tasks.add(rowToTask(rs))
                    }
                }
            }
            attachLabels(conn, tasks)
        }
    }

    /** 他ユーザーのtaskは見えない(所有権分離) */
    suspend fun findById(id: Long, userId: Long): Task? = withContext(Dispatchers.IO) {
        dataSource.connection.use { conn -> findByIdOnConnection(conn, id, userId) }
    }

    private fun findByIdOnConnection(conn: Connection, id: Long, userId: Long): Task? {
        conn.prepareStatement(
            "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at " +
                "FROM tasks WHERE id = ? AND user_id = ?",
        ).use { ps ->
            ps.setLong(1, id)
            ps.setLong(2, userId)
            ps.executeQuery().use { rs ->
                if (!rs.next()) {
                    return null
                }
                val tasks = mutableListOf(rowToTask(rs))
                return attachLabels(conn, tasks).first()
            }
        }
    }

    suspend fun create(userId: Long, input: TaskInput, status: TaskStatus): Long = withContext(Dispatchers.IO) {
        dataSource.connection.use { conn ->
            conn.autoCommit = false
            try {
                val now = LocalDateTime.now(ZoneOffset.UTC)
                val taskId: Long
                conn.prepareStatement(
                    "INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at) " +
                        "VALUES (?, ?, ?, ?, ?, ?, ?)",
                    Statement.RETURN_GENERATED_KEYS,
                ).use { ps ->
                    ps.setString(1, input.name)
                    setNullableString(ps, 2, input.description)
                    ps.setInt(3, status.dbValue)
                    ps.setObject(4, input.finishedOn)
                    ps.setLong(5, userId)
                    ps.setObject(6, now)
                    ps.setObject(7, now)
                    ps.executeUpdate()
                    ps.generatedKeys.use { keys ->
                        keys.next()
                        taskId = keys.getLong(1)
                    }
                }
                replaceLabels(conn, taskId, input.labelIds, now)
                conn.commit()
                taskId
            } catch (e: Exception) {
                conn.rollback()
                throw e
            } finally {
                conn.autoCommit = true
            }
        }
    }

    /** 戻り値: 更新できた場合true、対象行が(他人のtaskも含め)見つからない場合false */
    suspend fun update(id: Long, userId: Long, input: TaskInput, status: TaskStatus): Boolean =
        withContext(Dispatchers.IO) {
            dataSource.connection.use { conn ->
                conn.autoCommit = false
                try {
                    val now = LocalDateTime.now(ZoneOffset.UTC)
                    val updated: Int
                    conn.prepareStatement(
                        "UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, updated_at = ? " +
                            "WHERE id = ? AND user_id = ?",
                    ).use { ps ->
                        ps.setString(1, input.name)
                        setNullableString(ps, 2, input.description)
                        ps.setInt(3, status.dbValue)
                        ps.setObject(4, input.finishedOn)
                        ps.setObject(5, now)
                        ps.setLong(6, id)
                        ps.setLong(7, userId)
                        updated = ps.executeUpdate()
                    }
                    if (updated == 0) {
                        conn.rollback()
                        return@withContext false
                    }
                    replaceLabels(conn, id, input.labelIds, now)
                    conn.commit()
                    true
                } catch (e: Exception) {
                    conn.rollback()
                    throw e
                } finally {
                    conn.autoCommit = true
                }
            }
        }

    /**
     * tasksとtask_labelsの削除を1つのトランザクションで包む。
     * task_labelsには外部キー制約が無い(migrations/000004)ため、トランザクション無しで
     * 個別にDELETEすると、両文の間でプロセスが落ちた場合にtask_labelsの孤立行が残り得る
     * (backend-rust/backend-c/backend-cpp/backend-javaと同じ設計。backend-rustにはかつて
     * この保護が欠けている既知バグがあり、後に修正された経緯がある)
     */
    suspend fun delete(id: Long, userId: Long): Boolean = withContext(Dispatchers.IO) {
        dataSource.connection.use { conn ->
            conn.autoCommit = false
            try {
                val deleted: Int
                conn.prepareStatement("DELETE FROM tasks WHERE id = ? AND user_id = ?").use { ps ->
                    ps.setLong(1, id)
                    ps.setLong(2, userId)
                    deleted = ps.executeUpdate()
                }
                if (deleted == 0) {
                    conn.rollback()
                    return@withContext false
                }
                conn.prepareStatement("DELETE FROM task_labels WHERE task_id = ?").use { ps ->
                    ps.setLong(1, id)
                    ps.executeUpdate()
                }
                conn.commit()
                true
            } catch (e: Exception) {
                conn.rollback()
                throw e
            } finally {
                conn.autoCommit = true
            }
        }
    }

    /** JWT認証のuser_id解決用。ローカル発行issuerのsubはusers.idそのもの、存在確認のみ行う */
    suspend fun findUserById(id: Long): User? = withContext(Dispatchers.IO) {
        dataSource.connection.use { conn ->
            conn.prepareStatement("SELECT id, email, name FROM users WHERE id = ?").use { ps ->
                ps.setLong(1, id)
                ps.executeQuery().use { rs ->
                    if (!rs.next()) null else User(rs.getLong("id"), rs.getString("email"), rs.getString("name"))
                }
            }
        }
    }

    /**
     * Keycloak発行issuerのsub=keycloak_subは、usersテーブルには無くuser_keycloaksテーブルに
     * 分離されている(migration 000008_split_user_credentials、CONTRACT.mdセクション16.2)ため
     * JOIN経由で引く
     */
    suspend fun findUserByKeycloakSub(keycloakSub: String): User? = withContext(Dispatchers.IO) {
        dataSource.connection.use { conn ->
            conn.prepareStatement(
                "SELECT users.id AS id, users.email AS email, users.name AS name " +
                    "FROM users JOIN user_keycloaks ON user_keycloaks.user_id = users.id " +
                    "WHERE user_keycloaks.keycloak_sub = ?",
            ).use { ps ->
                ps.setString(1, keycloakSub)
                ps.executeQuery().use { rs ->
                    if (!rs.next()) null else User(rs.getLong("id"), rs.getString("email"), rs.getString("name"))
                }
            }
        }
    }

    private fun replaceLabels(conn: Connection, taskId: Long, labelIds: List<Long>, now: LocalDateTime) {
        conn.prepareStatement("DELETE FROM task_labels WHERE task_id = ?").use { ps ->
            ps.setLong(1, taskId)
            ps.executeUpdate()
        }
        // 【他言語で見つかった既知バグと同種】label_idsに同じidが重複して含まれる場合
        // (例: [3,3,5])、重複除去せずそのままINSERTすると2回目の(task_id,3)で
        // task_labelsの(task_id,label_id)へのUNIQUE制約(migrations/000004)に違反し、
        // 生のMySQLエラー(1062 Duplicate entry)がそのまま呼び出し元へ伝播してしまう
        // (backend(Go)・backend-rust・backend-javaで見つかった同根のバグ)
        val deduped = LinkedHashSet(labelIds)
        conn.prepareStatement(
            "INSERT INTO task_labels (task_id, label_id, created_at, updated_at) VALUES (?, ?, ?, ?)",
        ).use { ps ->
            for (labelId in deduped) {
                ps.setLong(1, taskId)
                ps.setLong(2, labelId)
                ps.setObject(3, now)
                ps.setObject(4, now)
                ps.addBatch()
            }
            if (deduped.isNotEmpty()) {
                ps.executeBatch()
            }
        }
    }

    private fun attachLabels(conn: Connection, tasks: List<Task>): List<Task> {
        if (tasks.isEmpty()) {
            return tasks
        }
        val ids = tasks.map { it.id }
        val placeholders = ids.joinToString(",") { "?" }
        val sql = "SELECT task_labels.task_id AS task_id, labels.id AS id, labels.name AS name " +
            "FROM task_labels JOIN labels ON labels.id = task_labels.label_id " +
            "WHERE task_labels.task_id IN ($placeholders)"

        val byTask = LinkedHashMap<Long, MutableList<Label>>()
        conn.prepareStatement(sql).use { ps ->
            for (i in ids.indices) {
                ps.setLong(i + 1, ids[i])
            }
            ps.executeQuery().use { rs ->
                while (rs.next()) {
                    val taskId = rs.getLong("task_id")
                    byTask.getOrPut(taskId) { mutableListOf() }
                        .add(Label(rs.getLong("id"), rs.getString("name")))
                }
            }
        }

        return tasks.map { t -> t.copy(labels = byTask.getOrDefault(t.id, emptyList())) }
    }

    private fun setNullableString(ps: PreparedStatement, index: Int, value: String?) {
        if (value == null) {
            ps.setNull(index, java.sql.Types.VARCHAR)
        } else {
            ps.setString(index, value)
        }
    }

    private fun rowToTask(rs: ResultSet): Task {
        val statusRaw = rs.getInt("status")
        val status = TaskStatus.fromDbValue(statusRaw) ?: TaskStatus.WAITING
        val finishedOn: LocalDate = rs.getObject("finished_on", LocalDate::class.java)
        return Task(
            id = rs.getLong("id"),
            name = rs.getString("name"),
            description = rs.getString("description"),
            status = status,
            finishedOn = finishedOn,
            userId = rs.getLong("user_id"),
            labels = emptyList(),
            // 【backend-javaと同じ既知の落とし穴】ResultSet#getTimestamp().toLocalDateTime()は
            // JVMのシステムデフォルトタイムゾーン経由で変換されるため、UTCの壁時計値が
            // 格納されているDATETIME列から実行環境のタイムゾーン分ずれた値を読んでしまう。
            // getObject(column, LocalDateTime::class.java)は列の生の日時要素を
            // タイムゾーン変換無しでそのままマッピングするため、これを使う(README.md参照)
            createdAt = rs.getObject("created_at", LocalDateTime::class.java),
            updatedAt = rs.getObject("updated_at", LocalDateTime::class.java),
        )
    }
}
