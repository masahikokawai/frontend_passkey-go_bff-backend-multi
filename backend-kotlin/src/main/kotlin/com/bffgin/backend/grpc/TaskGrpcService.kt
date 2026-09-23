package com.bffgin.backend.grpc

import com.bffgin.backend.auth.UserResolver
import com.bffgin.backend.domain.Task
import com.bffgin.backend.domain.TaskError
import com.bffgin.backend.domain.TaskValidation
import com.bffgin.backend.proto.CreateTaskRequest
import com.bffgin.backend.proto.DeleteTaskRequest
import com.bffgin.backend.proto.DeleteTaskResponse
import com.bffgin.backend.proto.GetTaskRequest
import com.bffgin.backend.proto.ListTasksRequest
import com.bffgin.backend.proto.ListTasksResponse
import com.bffgin.backend.proto.TaskServiceGrpcKt
import com.bffgin.backend.proto.UpdateTaskRequest
import com.bffgin.backend.repository.TaskRepository
import io.grpc.Status
import io.grpc.StatusRuntimeException
import org.slf4j.LoggerFactory
import java.time.LocalDate
import java.time.ZoneOffset

/**
 * 内部gRPC v2(:9102)。REST v1と同じRepository・ドメインモデル・認証(Dispatcher/UserResolver)を
 * 共有する(認証・バリデーション・トランザクション保護のロジックを複製しない設計、
 * backend-java/backend-rustのgrpcサービスと同じ方針)。
 *
 * grpc-kotlinのCoroutineImplBaseは、grpc-java(StreamObserverベースのコールバックAPI)と違い、
 * suspend funがそのままレスポンスを返す/例外を投げるだけでよい。手動でonNext/onCompletedを
 * 呼ぶ必要が無く、Kotlinのコルーチンが本来持つ「直線的に読めるコード」という利点が
 * gRPCサービス実装にもそのまま及ぶ
 */
class TaskGrpcService(
    private val repository: TaskRepository,
    private val userResolver: UserResolver,
) : TaskServiceGrpcKt.TaskServiceCoroutineImplBase() {

    private val log = LoggerFactory.getLogger(TaskGrpcService::class.java)

    override suspend fun listTasks(request: ListTasksRequest): ListTasksResponse = logged("list_tasks") {
        val userId = authenticate()
        val limit = if (request.limit <= 0) 20 else request.limit
        val tasks = repository.listCursor(userId, request.cursor, limit)

        val nextCursor = if (tasks.size < limit || tasks.isEmpty()) 0L else tasks.last().id

        val builder = ListTasksResponse.newBuilder().setNextCursor(nextCursor)
        for (t: Task in tasks) {
            builder.addTasks(TaskProtoMapper.toProto(t))
        }
        builder.build()
    }

    override suspend fun getTask(request: GetTaskRequest): com.bffgin.backend.proto.Task = logged("get_task") {
        val userId = authenticate()
        val task = repository.findById(request.id, userId) ?: throw TaskError.notFound()
        TaskProtoMapper.toProto(task)
    }

    override suspend fun createTask(request: CreateTaskRequest): com.bffgin.backend.proto.Task =
        logged("create_task") {
            val userId = authenticate()
            val input = TaskProtoMapper.fromCreateRequest(request)
            val today = LocalDate.now(ZoneOffset.UTC)
            val status = TaskValidation.validate(input, today)

            val id = repository.create(userId, input, status)
            val task = repository.findById(id, userId)
                ?: throw TaskError.internal("task disappeared after create")
            TaskProtoMapper.toProto(task)
        }

    override suspend fun updateTask(request: UpdateTaskRequest): com.bffgin.backend.proto.Task =
        logged("update_task") {
            val userId = authenticate()
            val input = TaskProtoMapper.fromUpdateRequest(request)
            val today = LocalDate.now(ZoneOffset.UTC)
            val status = TaskValidation.validate(input, today)

            val updated = repository.update(request.id, userId, input, status)
            if (!updated) {
                throw TaskError.notFound()
            }
            val task = repository.findById(request.id, userId) ?: throw TaskError.notFound()
            TaskProtoMapper.toProto(task)
        }

    override suspend fun deleteTask(request: DeleteTaskRequest): DeleteTaskResponse = logged("delete_task") {
        val userId = authenticate()
        val deleted = repository.delete(request.id, userId)
        if (!deleted) {
            throw TaskError.notFound()
        }
        DeleteTaskResponse.newBuilder().build()
    }

    /**
     * grpc-kotlinはRPC呼び出しをio.grpc.Context.current().asContextElement()相当で
     * コルーチンコンテキストへ伝播するため、suspend関数の中でもContext.Key#get()で
     * 通常のJava版と同じようにmetadataの値を読める(Context.current()自体はブロッキングしない
     * 同期呼び出しであり、コルーチンのディスパッチとは無関係)
     */
    private suspend fun authenticate(): Long {
        val authHeader = GrpcAuthInterceptor.AUTHORIZATION_CONTEXT_KEY.get()
        return userResolver.resolve(authHeader)
    }

    /**
     * method/実際のgRPCステータス(成否)/durationを1rpc1行のログとして出す
     * (backend-java/backend-rustのgrpcログ、backend-c/backend-cppのgrpc method=...と同じ形式)
     */
    private suspend fun <T> logged(method: String, block: suspend () -> T): T {
        val start = System.currentTimeMillis()
        var status = Status.OK
        try {
            val result = block()
            val durationMs = System.currentTimeMillis() - start
            log.info("grpc method={} status={} duration_ms={}", method, status.code, durationMs)
            return result
        } catch (e: TaskError) {
            status = toGrpcStatus(e)
            val durationMs = System.currentTimeMillis() - start
            log.info("grpc method={} status={} duration_ms={}", method, status.code, durationMs)
            throw status.withDescription(e.message).asRuntimeException()
        } catch (e: StatusRuntimeException) {
            status = e.status
            val durationMs = System.currentTimeMillis() - start
            log.info("grpc method={} status={} duration_ms={}", method, status.code, durationMs)
            throw e
        }
    }

    private fun toGrpcStatus(error: TaskError): Status = when (error.kind) {
        TaskError.Kind.UNAUTHORIZED -> Status.UNAUTHENTICATED
        TaskError.Kind.USER_NOT_PROVISIONED -> Status.PERMISSION_DENIED
        TaskError.Kind.NOT_FOUND -> Status.NOT_FOUND
        TaskError.Kind.INVALID_REQUEST, TaskError.Kind.INVALID_ID, TaskError.Kind.INVALID_STATUS,
        TaskError.Kind.INVALID_FINISHED_ON, TaskError.Kind.VALIDATION,
        -> Status.INVALID_ARGUMENT
        else -> Status.INTERNAL
    }
}
