package com.bffgin.backend.repository;

import com.bffgin.backend.DbTestFixture;
import com.bffgin.backend.domain.Task;
import com.bffgin.backend.domain.TaskInput;
import com.bffgin.backend.domain.TaskStatus;
import com.bffgin.backend.domain.User;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.time.LocalDate;
import java.util.List;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * 実DB(docker-compose上のMySQL)に接続する結合テスト。
 * backend-c/tests/task_repository_integration_test.c・
 * backend-cpp/tests/task_repository_integration_test.cppと同じシナリオを踏襲する
 */
class TaskRepositoryIntegrationTest {

    private DbTestFixture fixture;
    private TaskRepository repository;
    private long userId;
    private long labelIdA;
    private long labelIdB;

    @BeforeEach
    void setUp() throws Exception {
        fixture = new DbTestFixture();
        repository = new TaskRepository(fixture.dataSource);
        String suffix = System.nanoTime() + "-" + Math.abs(new java.util.Random().nextInt());
        userId = fixture.createUser(suffix);
        labelIdA = fixture.createLabel(suffix + "-a");
        labelIdB = fixture.createLabel(suffix + "-b");
    }

    @AfterEach
    void tearDown() {
        fixture.cleanupUser(userId);
        fixture.cleanupLabel(labelIdA);
        fixture.cleanupLabel(labelIdB);
        fixture.close();
    }

    @Test
    void createFindUpdateDeleteRoundTrip() throws Exception {
        TaskInput input = new TaskInput("task A", "desc", "waiting", LocalDate.of(2099, 1, 1),
                List.of(labelIdA, labelIdB));
        long id = repository.create(userId, input, TaskStatus.WAITING);

        Task created = repository.findById(id, userId).orElseThrow();
        assertEquals("task A", created.name());
        assertEquals("desc", created.description());
        assertEquals(TaskStatus.WAITING, created.status());
        assertEquals(LocalDate.of(2099, 1, 1), created.finishedOn());
        assertEquals(2, created.labels().size());

        TaskInput updateInput = new TaskInput("task A updated", null, "completed", LocalDate.of(2099, 2, 2),
                List.of(labelIdB));
        boolean updated = repository.update(id, userId, updateInput, TaskStatus.COMPLETED);
        assertTrue(updated);

        Task afterUpdate = repository.findById(id, userId).orElseThrow();
        assertEquals("task A updated", afterUpdate.name());
        assertEquals(null, afterUpdate.description());
        assertEquals(TaskStatus.COMPLETED, afterUpdate.status());
        assertEquals(1, afterUpdate.labels().size());

        boolean deleted = repository.delete(id, userId);
        assertTrue(deleted);
        assertTrue(repository.findById(id, userId).isEmpty());
    }

    @Test
    void updateOfNonExistentTaskReturnsFalse() throws Exception {
        TaskInput input = new TaskInput("x", null, "waiting", LocalDate.of(2099, 1, 1), List.of());
        boolean updated = repository.update(999_999_999L, userId, input, TaskStatus.WAITING);
        assertFalse(updated);
    }

    @Test
    void deleteOfNonExistentTaskReturnsFalse() throws Exception {
        boolean deleted = repository.delete(999_999_999L, userId);
        assertFalse(deleted);
    }

    /**
     * tasksとtask_labelsの削除が1つのトランザクションで包まれていることを確認する
     * (task_labelsに外部キー制約が無いため、これが無いと孤立行が残り得る既知バグクラス、
     * backend-rustで実際に見つかった経緯がある)
     */
    @Test
    void deleteRemovesTaskLabelsRows() throws Exception {
        TaskInput input = new TaskInput("task with labels", null, "waiting", LocalDate.of(2099, 1, 1),
                List.of(labelIdA, labelIdB));
        long id = repository.create(userId, input, TaskStatus.WAITING);

        assertEquals(2, countTaskLabels(id));
        repository.delete(id, userId);
        assertEquals(0, countTaskLabels(id));
    }

    @Test
    void createDedupsDuplicateLabelIds() throws Exception {
        TaskInput input = new TaskInput("dedup test", null, "waiting", LocalDate.of(2099, 1, 1),
                List.of(labelIdA, labelIdA, labelIdB));
        long id = repository.create(userId, input, TaskStatus.WAITING);

        Task task = repository.findById(id, userId).orElseThrow();
        assertEquals(2, task.labels().size());
        assertEquals(2, countTaskLabels(id));
    }

    @Test
    void updateDedupsDuplicateLabelIds() throws Exception {
        TaskInput input = new TaskInput("dedup update test", null, "waiting", LocalDate.of(2099, 1, 1), List.of());
        long id = repository.create(userId, input, TaskStatus.WAITING);

        TaskInput updateInput = new TaskInput("dedup update test", null, "waiting", LocalDate.of(2099, 1, 1),
                List.of(labelIdB, labelIdB, labelIdA));
        repository.update(id, userId, updateInput, TaskStatus.WAITING);

        assertEquals(2, countTaskLabels(id));
    }

    @Test
    void otherUserCannotSeeTask() throws Exception {
        String suffix = System.nanoTime() + "-other";
        long otherUserId = fixture.createUser(suffix);
        try {
            TaskInput input = new TaskInput("private task", null, "waiting", LocalDate.of(2099, 1, 1), List.of());
            long id = repository.create(userId, input, TaskStatus.WAITING);

            assertTrue(repository.findById(id, otherUserId).isEmpty());
            assertTrue(repository.findById(id, userId).isPresent());
        } finally {
            fixture.cleanupUser(otherUserId);
        }
    }

    @Test
    void listOffsetReturnsTotalAndRespectsLimitOffset() throws Exception {
        for (int i = 0; i < 3; i++) {
            TaskInput input = new TaskInput("offset-test-" + i, null, "waiting", LocalDate.of(2099, 1, 1), List.of());
            repository.create(userId, input, TaskStatus.WAITING);
        }
        var page1 = repository.listOffset(userId, 2, 0);
        assertEquals(3, page1.total());
        assertEquals(2, page1.tasks().size());

        var page2 = repository.listOffset(userId, 2, 2);
        assertEquals(3, page2.total());
        assertEquals(1, page2.tasks().size());
    }

    @Test
    void listCursorOrdersByIdAscendingAndRespectsAfterId() throws Exception {
        long id1 = repository.create(userId,
                new TaskInput("cursor-1", null, "waiting", LocalDate.of(2099, 1, 1), List.of()), TaskStatus.WAITING);
        long id2 = repository.create(userId,
                new TaskInput("cursor-2", null, "waiting", LocalDate.of(2099, 1, 1), List.of()), TaskStatus.WAITING);
        long id3 = repository.create(userId,
                new TaskInput("cursor-3", null, "waiting", LocalDate.of(2099, 1, 1), List.of()), TaskStatus.WAITING);

        List<Task> firstPage = repository.listCursor(userId, 0, 2);
        assertEquals(2, firstPage.size());
        assertEquals(id1, firstPage.get(0).id());
        assertEquals(id2, firstPage.get(1).id());

        List<Task> secondPage = repository.listCursor(userId, id2, 2);
        assertEquals(1, secondPage.size());
        assertEquals(id3, secondPage.get(0).id());
    }

    @Test
    void findUserByIdReturnsUserWhenExists() throws Exception {
        Optional<User> found = repository.findUserById(userId);
        assertTrue(found.isPresent());
        assertEquals(userId, found.get().id());
    }

    @Test
    void findUserByIdReturnsEmptyWhenNotFound() throws Exception {
        assertTrue(repository.findUserById(999_999_999L).isEmpty());
    }

    @Test
    void findUserByKeycloakSubReturnsUserWhenExists() throws Exception {
        String suffix = "kc-" + System.nanoTime();
        String keycloakSub = "keycloak-sub-" + suffix;
        long kcUserId = fixture.createUserWithKeycloakSub(suffix, keycloakSub);
        try {
            Optional<User> found = repository.findUserByKeycloakSub(keycloakSub);
            assertTrue(found.isPresent());
            assertEquals(kcUserId, found.get().id());
        } finally {
            fixture.cleanupUser(kcUserId);
        }
    }

    @Test
    void findUserByKeycloakSubReturnsEmptyWhenNotFound() throws Exception {
        assertTrue(repository.findUserByKeycloakSub("no-such-keycloak-sub").isEmpty());
    }

    private int countTaskLabels(long taskId) throws Exception {
        try (Connection conn = fixture.dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement("SELECT COUNT(*) FROM task_labels WHERE task_id = ?")) {
            ps.setLong(1, taskId);
            try (ResultSet rs = ps.executeQuery()) {
                rs.next();
                return rs.getInt(1);
            }
        }
    }
}
