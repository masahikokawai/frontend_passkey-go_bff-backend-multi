package com.bffgin.backend.rest

import com.bffgin.backend.auth.UserResolver
import com.bffgin.backend.domain.TaskError
import com.bffgin.backend.domain.TaskInput
import com.bffgin.backend.domain.TaskValidation
import com.bffgin.backend.repository.TaskRepository
import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import io.ktor.server.application.ApplicationCall
import io.ktor.server.application.call
import io.ktor.server.request.receiveText
import io.ktor.server.response.respond
import io.ktor.server.response.respondText
import io.ktor.server.routing.Route
import io.ktor.server.routing.delete
import io.ktor.server.routing.get
import io.ktor.server.routing.patch
import io.ktor.server.routing.post
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeParseException

/**
 * 内部REST v1(:8113配下)。バリデーション・エラー形状はbackend-java/backend-rustの
 * rest/task.rs・error.rsと1文字も変えていない(CONTRACT.mdセクション20.5のワイヤー契約パリティ)。
 * Ktorのルーティングは全てsuspend fun(coroutine context)で貫かれており、
 * ハンドラ内から呼ぶrepository/userResolverのメソッドもすべてsuspend funである
 */
private val mapper = ObjectMapper()

fun Route.taskRoutes(repository: TaskRepository, userResolver: UserResolver) {
    get("/internal/v1/tasks") {
        handleErrors(call) {
            val userId = authenticate(call, userResolver)
            val limit = call.request.queryParameters["limit"]?.toIntOrNull() ?: 20
            val offset = call.request.queryParameters["offset"]?.toIntOrNull() ?: 0

            val page = repository.listOffset(userId, limit, offset)
            val body = mapper.createObjectNode()
            val tasksNode = body.putArray("tasks")
            for (t in page.tasks) {
                tasksNode.add(TaskJson.toJson(t))
            }
            body.put("total", page.total)
            body.put("limit", limit)
            body.put("offset", offset)
            call.respondText(mapper.writeValueAsString(body), ContentType.Application.Json, HttpStatusCode.OK)
        }
    }

    get("/internal/v1/tasks/{id}") {
        handleErrors(call) {
            val userId = authenticate(call, userResolver)
            val id = parseId(call)
            val task = repository.findById(id, userId) ?: throw TaskError.notFound()
            call.respondText(
                mapper.writeValueAsString(TaskJson.toJson(task)),
                ContentType.Application.Json,
                HttpStatusCode.OK,
            )
        }
    }

    post("/internal/v1/tasks") {
        handleErrors(call) {
            val userId = authenticate(call, userResolver)
            val input = parseBody(call)
            val today = LocalDate.now(ZoneOffset.UTC)
            val status = TaskValidation.validate(input, today)

            val id = repository.create(userId, input, status)
            val task = repository.findById(id, userId) ?: throw TaskError.internal("task disappeared after create")
            call.respondText(
                mapper.writeValueAsString(TaskJson.toJson(task)),
                ContentType.Application.Json,
                HttpStatusCode.Created,
            )
        }
    }

    patch("/internal/v1/tasks/{id}") {
        handleErrors(call) {
            val userId = authenticate(call, userResolver)
            val id = parseId(call)
            val input = parseBody(call)
            val today = LocalDate.now(ZoneOffset.UTC)
            val status = TaskValidation.validate(input, today)

            val updated = repository.update(id, userId, input, status)
            if (!updated) {
                throw TaskError.notFound()
            }
            val task = repository.findById(id, userId) ?: throw TaskError.notFound()
            call.respondText(
                mapper.writeValueAsString(TaskJson.toJson(task)),
                ContentType.Application.Json,
                HttpStatusCode.OK,
            )
        }
    }

    delete("/internal/v1/tasks/{id}") {
        handleErrors(call) {
            val userId = authenticate(call, userResolver)
            val id = parseId(call)
            val deleted = repository.delete(id, userId)
            if (!deleted) {
                throw TaskError.notFound()
            }
            call.respond(HttpStatusCode.NoContent)
        }
    }
}

private suspend fun handleErrors(call: ApplicationCall, block: suspend () -> Unit) {
    try {
        block()
    } catch (e: TaskError) {
        RestErrorMapper.write(call, e)
    } catch (e: Exception) {
        RestErrorMapper.write(call, TaskError.internal(e.message ?: "internal error"))
    }
}

private suspend fun authenticate(call: ApplicationCall, userResolver: UserResolver): Long =
    userResolver.resolve(call.request.headers["Authorization"])

private fun parseId(call: ApplicationCall): Long =
    call.parameters["id"]?.toLongOrNull() ?: throw TaskError.invalidId()

/** backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い */
private suspend fun parseBody(call: ApplicationCall): TaskInput {
    val body: JsonNode? = try {
        mapper.readTree(call.receiveText())
    } catch (e: Exception) {
        throw TaskError.invalidRequest()
    }
    if (body == null || body.isNull) {
        throw TaskError.invalidRequest()
    }
    val name = body.path("name").asText("")
    val status = body.path("status").asText("")
    val finishedOnRaw = body.path("finished_on").asText("")
    if (name.isEmpty() || status.isEmpty() || finishedOnRaw.isEmpty()) {
        throw TaskError.invalidRequest()
    }
    val finishedOn: LocalDate = try {
        LocalDate.parse(finishedOnRaw)
    } catch (e: DateTimeParseException) {
        throw TaskError.invalidFinishedOn()
    }
    val description = if (body.hasNonNull("description")) body.get("description").asText() else null
    val labelIds = mutableListOf<Long>()
    if (body.has("label_ids") && body.get("label_ids").isArray) {
        for (idNode in body.get("label_ids")) {
            if (idNode.isIntegralNumber) {
                labelIds.add(idNode.asLong())
            }
        }
    }
    return TaskInput(name, description, status, finishedOn, labelIds)
}
