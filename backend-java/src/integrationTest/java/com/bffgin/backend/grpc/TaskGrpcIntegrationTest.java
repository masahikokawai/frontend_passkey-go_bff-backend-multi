package com.bffgin.backend.grpc;

import com.bffgin.backend.DbTestFixture;
import com.bffgin.backend.auth.Dispatcher;
import com.bffgin.backend.auth.HmacVerifier;
import com.bffgin.backend.auth.UserResolver;
import com.bffgin.backend.proto.CreateTaskRequest;
import com.bffgin.backend.proto.DeleteTaskRequest;
import com.bffgin.backend.proto.GetTaskRequest;
import com.bffgin.backend.proto.ListTasksRequest;
import com.bffgin.backend.proto.Task;
import com.bffgin.backend.proto.TaskServiceGrpc;
import com.bffgin.backend.repository.TaskRepository;
import com.bffgin.backend.test_support.TestTokenHelper;
import io.grpc.Grpc;
import io.grpc.InsecureChannelCredentials;
import io.grpc.InsecureServerCredentials;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Server;
import io.grpc.ServerInterceptors;
import io.grpc.StatusRuntimeException;
import io.grpc.stub.MetadataUtils;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

/**
 * 実DB(docker-compose上のMySQL)+実gRPCサーバーに対する結合テスト。
 * grpc-javaの生成スタブをそのまま使ってRPCを送る(backend-c/backend-cppの手書きgRPCクライアントと
 * 違い、Javaでは正規のクライアントコードでそのまま検証できる)
 */
class TaskGrpcIntegrationTest {

    private static final String HMAC_SECRET = "test-secret-at-least-32-bytes-long!!";
    private static final int TEST_PORT = 19101;

    private static DbTestFixture fixture;
    private static Server server;
    private static ManagedChannel channel;

    @BeforeAll
    static void startServer() throws Exception {
        fixture = new DbTestFixture();
        TaskRepository repository = new TaskRepository(fixture.dataSource);
        Dispatcher dispatcher = new Dispatcher()
                .register(Dispatcher.LOCAL_HMAC_ISSUER, new HmacVerifier(HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend"));
        UserResolver userResolver = new UserResolver(dispatcher, repository);
        TaskGrpcService service = new TaskGrpcService(repository, userResolver);

        server = Grpc.newServerBuilderForPort(TEST_PORT, InsecureServerCredentials.create())
                .addService(ServerInterceptors.intercept(service, new GrpcAuthInterceptor()))
                .build()
                .start();
        channel = Grpc.newChannelBuilderForAddress("127.0.0.1", TEST_PORT, InsecureChannelCredentials.create())
                .build();
    }

    @AfterAll
    static void stopServer() throws Exception {
        channel.shutdownNow();
        server.shutdownNow();
        fixture.close();
    }

    private long userId;
    private TaskServiceGrpc.TaskServiceBlockingStub stub;

    @BeforeEach
    void setUp() throws Exception {
        userId = fixture.createUser("grpc-" + System.nanoTime());
        String token = TestTokenHelper.makeHmacToken(HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend",
                String.valueOf(userId), 3600);
        Metadata headers = new Metadata();
        headers.put(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER), "Bearer " + token);
        stub = TaskServiceGrpc.newBlockingStub(channel)
                .withInterceptors(MetadataUtils.newAttachHeadersInterceptor(headers));
    }

    @AfterEach
    void tearDown() {
        fixture.cleanupUser(userId);
    }

    @Test
    void fullCrudRoundTrip() {
        Task created = stub.createTask(CreateTaskRequest.newBuilder()
                .setName("grpc task")
                .setStatus("waiting")
                .setFinishedOn("2099-01-01")
                .build());
        assertEquals("grpc task", created.getName());

        Task fetched = stub.getTask(GetTaskRequest.newBuilder().setId(created.getId()).build());
        assertEquals(created.getId(), fetched.getId());

        Task updated = stub.updateTask(com.bffgin.backend.proto.UpdateTaskRequest.newBuilder()
                .setId(created.getId())
                .setName("grpc task updated")
                .setStatus("completed")
                .setFinishedOn("2099-01-01")
                .build());
        assertEquals("grpc task updated", updated.getName());
        assertEquals("completed", updated.getStatus());

        stub.deleteTask(DeleteTaskRequest.newBuilder().setId(created.getId()).build());

        StatusRuntimeException ex = assertThrows(StatusRuntimeException.class,
                () -> stub.getTask(GetTaskRequest.newBuilder().setId(created.getId()).build()));
        assertEquals(io.grpc.Status.Code.NOT_FOUND, ex.getStatus().getCode());
    }

    @Test
    void deleteRemovesTaskLabelsRows() throws Exception {
        long labelId = fixture.createLabel("grpc-label-" + System.nanoTime());
        try {
            Task created = stub.createTask(CreateTaskRequest.newBuilder()
                    .setName("grpc labels test")
                    .setStatus("waiting")
                    .setFinishedOn("2099-01-01")
                    .addLabelIds(labelId)
                    .build());
            assertEquals(1, countTaskLabels(created.getId()));

            stub.deleteTask(DeleteTaskRequest.newBuilder().setId(created.getId()).build());
            assertEquals(0, countTaskLabels(created.getId()));
        } finally {
            fixture.cleanupLabel(labelId);
        }
    }

    @Test
    void unauthenticatedCallIsRejected() {
        TaskServiceGrpc.TaskServiceBlockingStub anonStub = TaskServiceGrpc.newBlockingStub(channel);
        StatusRuntimeException ex = assertThrows(StatusRuntimeException.class,
                () -> anonStub.listTasks(ListTasksRequest.newBuilder().setLimit(1).build()));
        assertEquals(io.grpc.Status.Code.UNAUTHENTICATED, ex.getStatus().getCode());
    }

    @Test
    void expiredTokenIsRejected() {
        String expiredToken = TestTokenHelper.makeHmacToken(HMAC_SECRET, Dispatcher.LOCAL_HMAC_ISSUER, "backend",
                String.valueOf(userId), -3600);
        Metadata headers = new Metadata();
        headers.put(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER), "Bearer " + expiredToken);
        TaskServiceGrpc.TaskServiceBlockingStub expiredStub = TaskServiceGrpc.newBlockingStub(channel)
                .withInterceptors(MetadataUtils.newAttachHeadersInterceptor(headers));

        StatusRuntimeException ex = assertThrows(StatusRuntimeException.class,
                () -> expiredStub.listTasks(ListTasksRequest.newBuilder().setLimit(1).build()));
        assertEquals(io.grpc.Status.Code.UNAUTHENTICATED, ex.getStatus().getCode());
    }

    @Test
    void listTasksCursorPaginationChainsToNoNextPage() {
        long id1 = stub.createTask(CreateTaskRequest.newBuilder().setName("c1").setStatus("waiting")
                .setFinishedOn("2099-01-01").build()).getId();
        long id2 = stub.createTask(CreateTaskRequest.newBuilder().setName("c2").setStatus("waiting")
                .setFinishedOn("2099-01-01").build()).getId();
        long id3 = stub.createTask(CreateTaskRequest.newBuilder().setName("c3").setStatus("waiting")
                .setFinishedOn("2099-01-01").build()).getId();

        var firstPage = stub.listTasks(ListTasksRequest.newBuilder().setLimit(2).build());
        assertEquals(2, firstPage.getTasksCount());
        assertEquals(id2, firstPage.getNextCursor());

        var secondPage = stub.listTasks(
                ListTasksRequest.newBuilder().setLimit(2).setCursor(firstPage.getNextCursor()).build());
        assertEquals(1, secondPage.getTasksCount());
        assertEquals(0, secondPage.getNextCursor());
        assertEquals(id3, secondPage.getTasks(0).getId());
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
