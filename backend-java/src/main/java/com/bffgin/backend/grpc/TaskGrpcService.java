package com.bffgin.backend.grpc;

import com.bffgin.backend.auth.UserResolver;
import com.bffgin.backend.domain.Task;
import com.bffgin.backend.domain.TaskError;
import com.bffgin.backend.domain.TaskStatus;
import com.bffgin.backend.domain.TaskValidation;
import com.bffgin.backend.proto.CreateTaskRequest;
import com.bffgin.backend.proto.DeleteTaskRequest;
import com.bffgin.backend.proto.DeleteTaskResponse;
import com.bffgin.backend.proto.GetTaskRequest;
import com.bffgin.backend.proto.ListTasksRequest;
import com.bffgin.backend.proto.ListTasksResponse;
import com.bffgin.backend.proto.TaskServiceGrpc;
import com.bffgin.backend.proto.UpdateTaskRequest;
import com.bffgin.backend.repository.TaskRepository;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.stub.StreamObserver;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.sql.SQLException;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.util.List;

/**
 * 内部gRPC v2(:9101)。REST v1と同じRepository・ドメインモデル・認証(Dispatcher/UserResolver)を
 * 共有する(認証・バリデーション・トランザクション保護のロジックを複製しない設計、
 * backend-rustのgrpc/task.rsと同じ方針)
 */
public final class TaskGrpcService extends TaskServiceGrpc.TaskServiceImplBase {

    private static final Logger log = LoggerFactory.getLogger(TaskGrpcService.class);

    private final TaskRepository repository;
    private final UserResolver userResolver;

    public TaskGrpcService(TaskRepository repository, UserResolver userResolver) {
        this.repository = repository;
        this.userResolver = userResolver;
    }

    @Override
    public void listTasks(ListTasksRequest request, StreamObserver<ListTasksResponse> observer) {
        run("list_tasks", observer, () -> {
            long userId = authenticate();
            int limit = request.getLimit() <= 0 ? 20 : request.getLimit();
            List<Task> tasks = repository.listCursor(userId, request.getCursor(), limit);

            long nextCursor = (tasks.size() < limit || tasks.isEmpty()) ? 0 : tasks.get(tasks.size() - 1).id();

            var builder = ListTasksResponse.newBuilder().setNextCursor(nextCursor);
            for (Task t : tasks) {
                builder.addTasks(TaskProtoMapper.toProto(t));
            }
            return builder.build();
        });
    }

    @Override
    public void getTask(GetTaskRequest request, StreamObserver<com.bffgin.backend.proto.Task> observer) {
        run("get_task", observer, () -> {
            long userId = authenticate();
            Task task = repository.findById(request.getId(), userId).orElseThrow(TaskError::notFound);
            return TaskProtoMapper.toProto(task);
        });
    }

    @Override
    public void createTask(CreateTaskRequest request, StreamObserver<com.bffgin.backend.proto.Task> observer) {
        run("create_task", observer, () -> {
            long userId = authenticate();
            var input = TaskProtoMapper.fromCreateRequest(request);
            LocalDate today = LocalDate.now(ZoneOffset.UTC);
            TaskStatus status = TaskValidation.validate(input, today);

            long id = repository.create(userId, input, status);
            Task task = repository.findById(id, userId)
                    .orElseThrow(() -> TaskError.internal("task disappeared after create"));
            return TaskProtoMapper.toProto(task);
        });
    }

    @Override
    public void updateTask(UpdateTaskRequest request, StreamObserver<com.bffgin.backend.proto.Task> observer) {
        run("update_task", observer, () -> {
            long userId = authenticate();
            var input = TaskProtoMapper.fromUpdateRequest(request);
            LocalDate today = LocalDate.now(ZoneOffset.UTC);
            TaskStatus status = TaskValidation.validate(input, today);

            boolean updated = repository.update(request.getId(), userId, input, status);
            if (!updated) {
                throw TaskError.notFound();
            }
            Task task = repository.findById(request.getId(), userId).orElseThrow(TaskError::notFound);
            return TaskProtoMapper.toProto(task);
        });
    }

    @Override
    public void deleteTask(DeleteTaskRequest request, StreamObserver<DeleteTaskResponse> observer) {
        run("delete_task", observer, () -> {
            long userId = authenticate();
            boolean deleted = repository.delete(request.getId(), userId);
            if (!deleted) {
                throw TaskError.notFound();
            }
            return DeleteTaskResponse.newBuilder().build();
        });
    }

    private long authenticate() throws TaskError {
        String authHeader = GrpcAuthInterceptor.AUTHORIZATION_CONTEXT_KEY.get();
        return userResolver.resolve(authHeader);
    }

    @FunctionalInterface
    private interface RpcCall<T> {
        T call() throws TaskError, SQLException;
    }

    /**
     * method/実際のgRPCステータス(成否)/durationを1rpc1行のログとして出す
     * (backend-rustのgrpc/task.rsのlogged()、backend-c/backend-cppのgrpc method=...と同じ形式)
     */
    private <T> void run(String method, StreamObserver<T> observer, RpcCall<T> call) {
        long start = System.currentTimeMillis();
        Status status = Status.OK;
        try {
            T response = call.call();
            observer.onNext(response);
            observer.onCompleted();
        } catch (TaskError e) {
            status = toGrpcStatus(e);
            observer.onError(status.withDescription(e.getMessage()).asRuntimeException());
        } catch (SQLException e) {
            status = Status.INTERNAL;
            observer.onError(status.withDescription(e.getMessage()).asRuntimeException());
        } catch (StatusRuntimeException e) {
            status = e.getStatus();
            observer.onError(e);
        }
        long durationMs = System.currentTimeMillis() - start;
        log.info("grpc method={} status={} duration_ms={}", method, status.getCode(), durationMs);
    }

    private static Status toGrpcStatus(TaskError error) {
        return switch (error.kind()) {
            case UNAUTHORIZED -> Status.UNAUTHENTICATED;
            case USER_NOT_PROVISIONED -> Status.PERMISSION_DENIED;
            case NOT_FOUND -> Status.NOT_FOUND;
            case INVALID_REQUEST, INVALID_ID, INVALID_STATUS, INVALID_FINISHED_ON, VALIDATION ->
                    Status.INVALID_ARGUMENT;
            default -> Status.INTERNAL;
        };
    }
}
