package com.bffgin.backend.grpc

import com.bffgin.backend.DbTestFixture
import com.bffgin.backend.auth.Dispatcher
import com.bffgin.backend.auth.HmacVerifier
import com.bffgin.backend.auth.UserResolver
import com.bffgin.backend.proto.CreateTaskRequest
import com.bffgin.backend.proto.DeleteTaskRequest
import com.bffgin.backend.proto.GetTaskRequest
import com.bffgin.backend.proto.ListTasksRequest
import com.bffgin.backend.proto.TaskServiceGrpcKt
import com.bffgin.backend.proto.UpdateTaskRequest
import com.bffgin.backend.repository.TaskRepository
import com.bffgin.backend.test_support.TestTokenHelper
import io.grpc.Grpc
import io.grpc.InsecureChannelCredentials
import io.grpc.InsecureServerCredentials
import io.grpc.ManagedChannel
import io.grpc.Metadata
import io.grpc.Server
import io.grpc.ServerInterceptors
import io.grpc.StatusException
import io.grpc.stub.MetadataUtils
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.AfterAll
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.BeforeAll
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test

/**
 * 実DB(docker-compose上のMySQL)+実gRPCサーバーに対する結合テスト。
 * grpc-kotlinの生成コルーチンスタブ(TaskServiceCoroutineStub)をそのまま使ってRPCを送る
 * (backend-c/backend-cppの手書きgRPCクライアントと違い、Kotlinでも正規のクライアントコードで
 * そのまま検証できる)
 */
class TaskGrpcIntegrationTest {

    companion object {
        private const val HMAC_SECRET = "test-secret-at-least-32-bytes-long!!"
        private const val TEST_PORT = 19102

        private lateinit var fixture: DbTestFixture
        private lateinit var server: Server
        private lateinit var channel: ManagedChannel

        @JvmStatic
        @BeforeAll
        fun startServer() {
            fixture = DbTestFixture()
            val repository = TaskRepository(fixture.dataSource)
            val dispatcher = Dispatcher()
                .register(Dispatcher.LOCAL_HMAC_ISSUER, HmacVerifier(HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend"))
            val userResolver = UserResolver(dispatcher, repository)
            val service = TaskGrpcService(repository, userResolver)

            server = Grpc.newServerBuilderForPort(TEST_PORT, InsecureServerCredentials.create())
                .addService(ServerInterceptors.intercept(service, GrpcAuthInterceptor()))
                .build()
                .start()
            channel = Grpc.newChannelBuilderForAddress("127.0.0.1", TEST_PORT, InsecureChannelCredentials.create())
                .build()
        }

        @JvmStatic
        @AfterAll
        fun stopServer() {
            channel.shutdownNow()
            server.shutdownNow()
            fixture.close()
        }
    }

    private var userId: Long = 0
    private lateinit var stub: TaskServiceGrpcKt.TaskServiceCoroutineStub

    @BeforeEach
    fun setUp() {
        userId = fixture.createUser("grpc-${System.nanoTime()}")
        val token = TestTokenHelper.makeHmacToken(HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend", userId.toString(), 3600)
        val headers = Metadata()
        headers.put(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER), "Bearer $token")
        stub = TaskServiceGrpcKt.TaskServiceCoroutineStub(channel)
            .withInterceptors(MetadataUtils.newAttachHeadersInterceptor(headers))
    }

    @AfterEach
    fun tearDown() {
        fixture.cleanupUser(userId)
    }

    @Test
    fun fullCrudRoundTrip() = runBlocking {
        val created = stub.createTask(
            CreateTaskRequest.newBuilder().setName("grpc task").setStatus("waiting").setFinishedOn("2099-01-01").build(),
        )
        assertEquals("grpc task", created.name)

        val fetched = stub.getTask(GetTaskRequest.newBuilder().setId(created.id).build())
        assertEquals(created.id, fetched.id)

        val updated = stub.updateTask(
            UpdateTaskRequest.newBuilder().setId(created.id).setName("grpc task updated").setStatus("completed")
                .setFinishedOn("2099-01-01").build(),
        )
        assertEquals("grpc task updated", updated.name)
        assertEquals("completed", updated.status)

        stub.deleteTask(DeleteTaskRequest.newBuilder().setId(created.id).build())

        val ex = assertThrows(StatusException::class.java) {
            runBlocking { stub.getTask(GetTaskRequest.newBuilder().setId(created.id).build()) }
        }
        assertEquals(io.grpc.Status.Code.NOT_FOUND, ex.status.code)
    }

    @Test
    fun deleteRemovesTaskLabelsRows() = runBlocking {
        val labelId = fixture.createLabel("grpc-label-${System.nanoTime()}")
        try {
            val created = stub.createTask(
                CreateTaskRequest.newBuilder().setName("grpc labels test").setStatus("waiting")
                    .setFinishedOn("2099-01-01").addLabelIds(labelId).build(),
            )
            assertEquals(1, countTaskLabels(created.id))

            stub.deleteTask(DeleteTaskRequest.newBuilder().setId(created.id).build())
            assertEquals(0, countTaskLabels(created.id))
        } finally {
            fixture.cleanupLabel(labelId)
        }
    }

    @Test
    fun unauthenticatedCallIsRejected() {
        val anonStub = TaskServiceGrpcKt.TaskServiceCoroutineStub(channel)
        val ex = assertThrows(StatusException::class.java) {
            runBlocking { anonStub.listTasks(ListTasksRequest.newBuilder().setLimit(1).build()) }
        }
        assertEquals(io.grpc.Status.Code.UNAUTHENTICATED, ex.status.code)
    }

    @Test
    fun expiredTokenIsRejected() {
        val expiredToken = TestTokenHelper.makeHmacToken(
            HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend", userId.toString(), -3600,
        )
        val headers = Metadata()
        headers.put(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER), "Bearer $expiredToken")
        val expiredStub = TaskServiceGrpcKt.TaskServiceCoroutineStub(channel)
            .withInterceptors(MetadataUtils.newAttachHeadersInterceptor(headers))

        val ex = assertThrows(StatusException::class.java) {
            runBlocking { expiredStub.listTasks(ListTasksRequest.newBuilder().setLimit(1).build()) }
        }
        assertEquals(io.grpc.Status.Code.UNAUTHENTICATED, ex.status.code)
    }

    @Test
    fun listTasksCursorPaginationChainsToNoNextPage() = runBlocking {
        val id1 = stub.createTask(
            CreateTaskRequest.newBuilder().setName("c1").setStatus("waiting").setFinishedOn("2099-01-01").build(),
        ).id
        val id2 = stub.createTask(
            CreateTaskRequest.newBuilder().setName("c2").setStatus("waiting").setFinishedOn("2099-01-01").build(),
        ).id
        val id3 = stub.createTask(
            CreateTaskRequest.newBuilder().setName("c3").setStatus("waiting").setFinishedOn("2099-01-01").build(),
        ).id

        val firstPage = stub.listTasks(ListTasksRequest.newBuilder().setLimit(2).build())
        assertEquals(2, firstPage.tasksCount)
        assertEquals(id2, firstPage.nextCursor)

        val secondPage = stub.listTasks(ListTasksRequest.newBuilder().setLimit(2).setCursor(firstPage.nextCursor).build())
        assertEquals(1, secondPage.tasksCount)
        assertEquals(0, secondPage.nextCursor)
        assertEquals(id3, secondPage.getTasks(0).id)
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
