package com.bffgin.backend.repository

import com.bffgin.backend.DbTestFixture
import com.bffgin.backend.domain.TaskInput
import com.bffgin.backend.domain.TaskStatus
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import java.time.LocalDate

/**
 * 実DB(docker-compose上のMySQL)に接続する結合テスト。
 * backend-java/backend-c/backend-cppのtask_repository_integration_testと同じシナリオを踏襲する。
 * TaskRepositoryのメソッドはsuspend funのため、各テストはrunBlockingで実行する
 * (実DB/実ネットワークを相手にするため仮想時間のrunTestではなくrunBlockingを使う)
 */
class TaskRepositoryIntegrationTest {

    private lateinit var fixture: DbTestFixture
    private lateinit var repository: TaskRepository
    private var userId: Long = 0
    private var labelIdA: Long = 0
    private var labelIdB: Long = 0

    @BeforeEach
    fun setUp() {
        fixture = DbTestFixture()
        repository = TaskRepository(fixture.dataSource)
        val suffix = "${System.nanoTime()}-${Math.abs(java.util.Random().nextInt())}"
        userId = fixture.createUser(suffix)
        labelIdA = fixture.createLabel("$suffix-a")
        labelIdB = fixture.createLabel("$suffix-b")
    }

    @AfterEach
    fun tearDown() {
        fixture.cleanupUser(userId)
        fixture.cleanupLabel(labelIdA)
        fixture.cleanupLabel(labelIdB)
        fixture.close()
    }

    @Test
    fun createFindUpdateDeleteRoundTrip() = runBlocking {
        val input = TaskInput("task A", "desc", "waiting", LocalDate.of(2099, 1, 1), listOf(labelIdA, labelIdB))
        val id = repository.create(userId, input, TaskStatus.WAITING)

        val created = repository.findById(id, userId)
        assertNotNull(created)
        assertEquals("task A", created!!.name)
        assertEquals("desc", created.description)
        assertEquals(TaskStatus.WAITING, created.status)
        assertEquals(LocalDate.of(2099, 1, 1), created.finishedOn)
        assertEquals(2, created.labels.size)

        val updateInput = TaskInput("task A updated", null, "completed", LocalDate.of(2099, 2, 2), listOf(labelIdB))
        val updated = repository.update(id, userId, updateInput, TaskStatus.COMPLETED)
        assertTrue(updated)

        val afterUpdate = repository.findById(id, userId)
        assertNotNull(afterUpdate)
        assertEquals("task A updated", afterUpdate!!.name)
        assertNull(afterUpdate.description)
        assertEquals(TaskStatus.COMPLETED, afterUpdate.status)
        assertEquals(1, afterUpdate.labels.size)

        val deleted = repository.delete(id, userId)
        assertTrue(deleted)
        assertNull(repository.findById(id, userId))
    }

    @Test
    fun updateOfNonExistentTaskReturnsFalse() = runBlocking {
        val input = TaskInput("x", null, "waiting", LocalDate.of(2099, 1, 1), emptyList())
        val updated = repository.update(999_999_999L, userId, input, TaskStatus.WAITING)
        assertFalse(updated)
    }

    @Test
    fun deleteOfNonExistentTaskReturnsFalse() = runBlocking {
        val deleted = repository.delete(999_999_999L, userId)
        assertFalse(deleted)
    }

    /**
     * tasksとtask_labelsの削除が1つのトランザクションで包まれていることを確認する
     * (task_labelsに外部キー制約が無いため、これが無いと孤立行が残り得る既知バグクラス、
     * backend-rustで実際に見つかった経緯がある)
     */
    @Test
    fun deleteRemovesTaskLabelsRows() = runBlocking {
        val input = TaskInput("task with labels", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelIdA, labelIdB))
        val id = repository.create(userId, input, TaskStatus.WAITING)

        assertEquals(2, countTaskLabels(id))
        repository.delete(id, userId)
        assertEquals(0, countTaskLabels(id))
    }

    @Test
    fun createDedupsDuplicateLabelIds() = runBlocking {
        val input = TaskInput("dedup test", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelIdA, labelIdA, labelIdB))
        val id = repository.create(userId, input, TaskStatus.WAITING)

        val task = repository.findById(id, userId)
        assertNotNull(task)
        assertEquals(2, task!!.labels.size)
        assertEquals(2, countTaskLabels(id))
    }

    @Test
    fun updateDedupsDuplicateLabelIds() = runBlocking {
        val input = TaskInput("dedup update test", null, "waiting", LocalDate.of(2099, 1, 1), emptyList())
        val id = repository.create(userId, input, TaskStatus.WAITING)

        val updateInput = TaskInput(
            "dedup update test", null, "waiting", LocalDate.of(2099, 1, 1), listOf(labelIdB, labelIdB, labelIdA),
        )
        repository.update(id, userId, updateInput, TaskStatus.WAITING)

        assertEquals(2, countTaskLabels(id))
    }

    @Test
    fun otherUserCannotSeeTask() = runBlocking {
        val suffix = "${System.nanoTime()}-other"
        val otherUserId = fixture.createUser(suffix)
        try {
            val input = TaskInput("private task", null, "waiting", LocalDate.of(2099, 1, 1), emptyList())
            val id = repository.create(userId, input, TaskStatus.WAITING)

            assertNull(repository.findById(id, otherUserId))
            assertNotNull(repository.findById(id, userId))
        } finally {
            fixture.cleanupUser(otherUserId)
        }
    }

    @Test
    fun listOffsetReturnsTotalAndRespectsLimitOffset() = runBlocking {
        for (i in 0 until 3) {
            val input = TaskInput("offset-test-$i", null, "waiting", LocalDate.of(2099, 1, 1), emptyList())
            repository.create(userId, input, TaskStatus.WAITING)
        }
        val page1 = repository.listOffset(userId, 2, 0)
        assertEquals(3, page1.total)
        assertEquals(2, page1.tasks.size)

        val page2 = repository.listOffset(userId, 2, 2)
        assertEquals(3, page2.total)
        assertEquals(1, page2.tasks.size)
    }

    @Test
    fun listCursorOrdersByIdAscendingAndRespectsAfterId() = runBlocking {
        val id1 = repository.create(
            userId, TaskInput("cursor-1", null, "waiting", LocalDate.of(2099, 1, 1), emptyList()), TaskStatus.WAITING,
        )
        val id2 = repository.create(
            userId, TaskInput("cursor-2", null, "waiting", LocalDate.of(2099, 1, 1), emptyList()), TaskStatus.WAITING,
        )
        val id3 = repository.create(
            userId, TaskInput("cursor-3", null, "waiting", LocalDate.of(2099, 1, 1), emptyList()), TaskStatus.WAITING,
        )

        val firstPage = repository.listCursor(userId, 0, 2)
        assertEquals(2, firstPage.size)
        assertEquals(id1, firstPage[0].id)
        assertEquals(id2, firstPage[1].id)

        val secondPage = repository.listCursor(userId, id2, 2)
        assertEquals(1, secondPage.size)
        assertEquals(id3, secondPage[0].id)
    }

    @Test
    fun findUserByIdReturnsUserWhenExists() = runBlocking {
        val found = repository.findUserById(userId)
        assertNotNull(found)
        assertEquals(userId, found!!.id)
    }

    @Test
    fun findUserByIdReturnsNullWhenNotFound() = runBlocking {
        assertNull(repository.findUserById(999_999_999L))
    }

    @Test
    fun findUserByKeycloakSubReturnsUserWhenExists() = runBlocking {
        val suffix = "kc-${System.nanoTime()}"
        val keycloakSub = "keycloak-sub-$suffix"
        val kcUserId = fixture.createUserWithKeycloakSub(suffix, keycloakSub)
        try {
            val found = repository.findUserByKeycloakSub(keycloakSub)
            assertNotNull(found)
            assertEquals(kcUserId, found!!.id)
        } finally {
            fixture.cleanupUser(kcUserId)
        }
    }

    @Test
    fun findUserByKeycloakSubReturnsNullWhenNotFound() = runBlocking {
        assertNull(repository.findUserByKeycloakSub("no-such-keycloak-sub"))
    }

    private fun countTaskLabels(taskId: Long): Int {
        fixture.dataSource.connection.use { conn ->
            conn.prepareStatement("SELECT COUNT(*) FROM task_labels WHERE task_id = ?").use { ps ->
                ps.setLong(1, taskId)
                ps.executeQuery().use { rs ->
                    rs.next()
                    return rs.getInt(1)
                }
            }
        }
    }
}
