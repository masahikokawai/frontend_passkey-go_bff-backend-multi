package com.bffgin.backend.repository;

import com.bffgin.backend.domain.Label;
import com.bffgin.backend.domain.Task;
import com.bffgin.backend.domain.TaskInput;
import com.bffgin.backend.domain.TaskStatus;
import com.bffgin.backend.domain.User;

import javax.sql.DataSource;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * 生JDBC + PreparedStatementのみ(ORM禁止方針、他言語と統一)。
 * コネクションプールはHikariCPが担う(このクラス自体はDataSourceを受け取るだけ)
 */
public final class TaskRepository {

    private final DataSource dataSource;

    public TaskRepository(DataSource dataSource) {
        this.dataSource = dataSource;
    }

    public record OffsetPage(List<Task> tasks, long total) {
    }

    /** v1(REST)向け: offsetページング */
    public OffsetPage listOffset(long userId, int limit, int offset) throws SQLException {
        try (Connection conn = dataSource.getConnection()) {
            long total;
            try (PreparedStatement ps = conn.prepareStatement(
                    "SELECT COUNT(*) FROM tasks WHERE user_id = ?")) {
                ps.setLong(1, userId);
                try (ResultSet rs = ps.executeQuery()) {
                    rs.next();
                    total = rs.getLong(1);
                }
            }

            List<Task> tasks = new ArrayList<>();
            try (PreparedStatement ps = conn.prepareStatement(
                    "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at "
                            + "FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?")) {
                ps.setLong(1, userId);
                ps.setInt(2, limit);
                ps.setInt(3, offset);
                try (ResultSet rs = ps.executeQuery()) {
                    while (rs.next()) {
                        tasks.add(rowToTask(rs));
                    }
                }
            }
            attachLabels(conn, tasks);
            return new OffsetPage(tasks, total);
        }
    }

    /**
     * v2(gRPC)向け: id昇順のkeyset(cursor)ページング。
     * cursorの実体はuint64(直前ページ最後のtask.id、0=先頭)、合成キーは使わない
     * (CONTRACT.mdセクション5)
     */
    public List<Task> listCursor(long userId, long afterId, int limit) throws SQLException {
        try (Connection conn = dataSource.getConnection()) {
            String sql = "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at "
                    + "FROM tasks WHERE user_id = ?"
                    + (afterId > 0 ? " AND id > ?" : "")
                    + " ORDER BY id ASC LIMIT ?";
            List<Task> tasks = new ArrayList<>();
            try (PreparedStatement ps = conn.prepareStatement(sql)) {
                int idx = 1;
                ps.setLong(idx++, userId);
                if (afterId > 0) {
                    ps.setLong(idx++, afterId);
                }
                ps.setInt(idx, limit);
                try (ResultSet rs = ps.executeQuery()) {
                    while (rs.next()) {
                        tasks.add(rowToTask(rs));
                    }
                }
            }
            attachLabels(conn, tasks);
            return tasks;
        }
    }

    /** 他ユーザーのtaskは見えない(所有権分離) */
    public Optional<Task> findById(long id, long userId) throws SQLException {
        try (Connection conn = dataSource.getConnection()) {
            return findByIdOnConnection(conn, id, userId);
        }
    }

    private Optional<Task> findByIdOnConnection(Connection conn, long id, long userId) throws SQLException {
        try (PreparedStatement ps = conn.prepareStatement(
                "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at "
                        + "FROM tasks WHERE id = ? AND user_id = ?")) {
            ps.setLong(1, id);
            ps.setLong(2, userId);
            try (ResultSet rs = ps.executeQuery()) {
                if (!rs.next()) {
                    return Optional.empty();
                }
                List<Task> tasks = new ArrayList<>();
                tasks.add(rowToTask(rs));
                attachLabels(conn, tasks);
                return Optional.of(tasks.get(0));
            }
        }
    }

    public long create(long userId, TaskInput input, TaskStatus status) throws SQLException {
        try (Connection conn = dataSource.getConnection()) {
            conn.setAutoCommit(false);
            try {
                LocalDateTime now = LocalDateTime.now(java.time.ZoneOffset.UTC);
                long taskId;
                try (PreparedStatement ps = conn.prepareStatement(
                        "INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at) "
                                + "VALUES (?, ?, ?, ?, ?, ?, ?)",
                        Statement.RETURN_GENERATED_KEYS)) {
                    ps.setString(1, input.name());
                    setNullableString(ps, 2, input.description());
                    ps.setInt(3, status.dbValue());
                    ps.setObject(4, input.finishedOn());
                    ps.setLong(5, userId);
                    ps.setObject(6, now);
                    ps.setObject(7, now);
                    ps.executeUpdate();
                    try (ResultSet keys = ps.getGeneratedKeys()) {
                        keys.next();
                        taskId = keys.getLong(1);
                    }
                }
                replaceLabels(conn, taskId, input.labelIds(), now);
                conn.commit();
                return taskId;
            } catch (SQLException e) {
                conn.rollback();
                throw e;
            } finally {
                conn.setAutoCommit(true);
            }
        }
    }

    /** 戻り値: 更新できた場合true、対象行が(他人のtaskも含め)見つからない場合false */
    public boolean update(long id, long userId, TaskInput input, TaskStatus status) throws SQLException {
        try (Connection conn = dataSource.getConnection()) {
            conn.setAutoCommit(false);
            try {
                LocalDateTime now = LocalDateTime.now(java.time.ZoneOffset.UTC);
                int updated;
                try (PreparedStatement ps = conn.prepareStatement(
                        "UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, updated_at = ? "
                                + "WHERE id = ? AND user_id = ?")) {
                    ps.setString(1, input.name());
                    setNullableString(ps, 2, input.description());
                    ps.setInt(3, status.dbValue());
                    ps.setObject(4, input.finishedOn());
                    ps.setObject(5, now);
                    ps.setLong(6, id);
                    ps.setLong(7, userId);
                    updated = ps.executeUpdate();
                }
                if (updated == 0) {
                    conn.rollback();
                    return false;
                }
                replaceLabels(conn, id, input.labelIds(), now);
                conn.commit();
                return true;
            } catch (SQLException e) {
                conn.rollback();
                throw e;
            } finally {
                conn.setAutoCommit(true);
            }
        }
    }

    /**
     * tasksとtask_labelsの削除を1つのトランザクションで包む。
     * task_labelsには外部キー制約が無い(migrations/000004)ため、トランザクション無しで
     * 個別にDELETEすると、両文の間でプロセスが落ちた場合にtask_labelsの孤立行が残り得る
     * (backend-rust/backend-c/backend-cppと同じ設計、backend-rustのdb.rsのコメント参照。
     * かつてbackend-rustにはこの保護が欠けている既知バグがあり、後に修正された経緯がある)
     */
    public boolean delete(long id, long userId) throws SQLException {
        try (Connection conn = dataSource.getConnection()) {
            conn.setAutoCommit(false);
            try {
                int deleted;
                try (PreparedStatement ps = conn.prepareStatement(
                        "DELETE FROM tasks WHERE id = ? AND user_id = ?")) {
                    ps.setLong(1, id);
                    ps.setLong(2, userId);
                    deleted = ps.executeUpdate();
                }
                if (deleted == 0) {
                    conn.rollback();
                    return false;
                }
                try (PreparedStatement ps = conn.prepareStatement(
                        "DELETE FROM task_labels WHERE task_id = ?")) {
                    ps.setLong(1, id);
                    ps.executeUpdate();
                }
                conn.commit();
                return true;
            } catch (SQLException e) {
                conn.rollback();
                throw e;
            } finally {
                conn.setAutoCommit(true);
            }
        }
    }

    /** JWT認証のuser_id解決用。ローカル発行issuerのsubはusers.idそのもの、存在確認のみ行う */
    public Optional<User> findUserById(long id) throws SQLException {
        try (Connection conn = dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "SELECT id, email, name FROM users WHERE id = ?")) {
            ps.setLong(1, id);
            try (ResultSet rs = ps.executeQuery()) {
                if (!rs.next()) {
                    return Optional.empty();
                }
                return Optional.of(new User(rs.getLong("id"), rs.getString("email"), rs.getString("name")));
            }
        }
    }

    /**
     * Keycloak発行issuerのsub=keycloak_subは、usersテーブルには無くuser_keycloaksテーブルに
     * 分離されている(migration 000008_split_user_credentials、CONTRACT.mdセクション16.2)ため
     * JOIN経由で引く
     */
    public Optional<User> findUserByKeycloakSub(String keycloakSub) throws SQLException {
        try (Connection conn = dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "SELECT users.id AS id, users.email AS email, users.name AS name "
                                + "FROM users JOIN user_keycloaks ON user_keycloaks.user_id = users.id "
                                + "WHERE user_keycloaks.keycloak_sub = ?")) {
            ps.setString(1, keycloakSub);
            try (ResultSet rs = ps.executeQuery()) {
                if (!rs.next()) {
                    return Optional.empty();
                }
                return Optional.of(new User(rs.getLong("id"), rs.getString("email"), rs.getString("name")));
            }
        }
    }

    private void replaceLabels(Connection conn, long taskId, List<Long> labelIds, LocalDateTime now)
            throws SQLException {
        try (PreparedStatement ps = conn.prepareStatement("DELETE FROM task_labels WHERE task_id = ?")) {
            ps.setLong(1, taskId);
            ps.executeUpdate();
        }
        // 【他言語で見つかった既知バグと同種】label_idsに同じidが重複して含まれる場合
        // (例: [3,3,5])、重複除去せずそのままINSERTすると2回目の(task_id,3)で
        // task_labelsの(task_id,label_id)へのUNIQUE制約(migrations/000004)に違反し、
        // 生のMySQLエラー(1062 Duplicate entry)がそのまま呼び出し元へ伝播してしまう
        // (backend(Go)・backend-rustの両方で見つかった同根のバグ、backend-rustのdb.rsのコメント参照)
        LinkedHashSet<Long> deduped = new LinkedHashSet<>(labelIds);
        try (PreparedStatement ps = conn.prepareStatement(
                "INSERT INTO task_labels (task_id, label_id, created_at, updated_at) VALUES (?, ?, ?, ?)")) {
            for (Long labelId : deduped) {
                ps.setLong(1, taskId);
                ps.setLong(2, labelId);
                ps.setObject(3, now);
                ps.setObject(4, now);
                ps.addBatch();
            }
            if (!deduped.isEmpty()) {
                ps.executeBatch();
            }
        }
    }

    private void attachLabels(Connection conn, List<Task> tasks) throws SQLException {
        if (tasks.isEmpty()) {
            return;
        }
        List<Long> ids = tasks.stream().map(Task::id).toList();
        String placeholders = String.join(",", ids.stream().map(i -> "?").toList());
        String sql = "SELECT task_labels.task_id AS task_id, labels.id AS id, labels.name AS name "
                + "FROM task_labels JOIN labels ON labels.id = task_labels.label_id "
                + "WHERE task_labels.task_id IN (" + placeholders + ")";

        Map<Long, List<Label>> byTask = new LinkedHashMap<>();
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            for (int i = 0; i < ids.size(); i++) {
                ps.setLong(i + 1, ids.get(i));
            }
            try (ResultSet rs = ps.executeQuery()) {
                while (rs.next()) {
                    long taskId = rs.getLong("task_id");
                    byTask.computeIfAbsent(taskId, k -> new ArrayList<>())
                            .add(new Label(rs.getLong("id"), rs.getString("name")));
                }
            }
        }

        for (int i = 0; i < tasks.size(); i++) {
            Task t = tasks.get(i);
            List<Label> labels = byTask.getOrDefault(t.id(), List.of());
            tasks.set(i, new Task(t.id(), t.name(), t.description(), t.status(), t.finishedOn(), t.userId(),
                    labels, t.createdAt(), t.updatedAt()));
        }
    }

    private static void setNullableString(PreparedStatement ps, int index, String value) throws SQLException {
        if (value == null) {
            ps.setNull(index, java.sql.Types.VARCHAR);
        } else {
            ps.setString(index, value);
        }
    }

    private static Task rowToTask(ResultSet rs) throws SQLException {
        int statusRaw = rs.getInt("status");
        TaskStatus status = TaskStatus.fromDbValue(statusRaw).orElse(TaskStatus.WAITING);
        LocalDate finishedOn = rs.getObject("finished_on", LocalDate.class);
        return new Task(
                rs.getLong("id"),
                rs.getString("name"),
                rs.getString("description"),
                status,
                finishedOn,
                rs.getLong("user_id"),
                List.of(),
                rs.getObject("created_at", LocalDateTime.class),
                rs.getObject("updated_at", LocalDateTime.class));
    }
}
