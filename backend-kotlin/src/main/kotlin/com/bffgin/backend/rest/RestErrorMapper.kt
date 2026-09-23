package com.bffgin.backend.rest

import com.bffgin.backend.domain.TaskError
import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.databind.node.ObjectNode
import io.ktor.http.HttpStatusCode
import io.ktor.http.ContentType
import io.ktor.server.application.ApplicationCall
import io.ktor.server.response.respondText

/**
 * backend-java/backend-rustのerror.rsと1文字も変えていないJSON形状・HTTPステータス。
 * ワイヤー契約パリティの原則(CONTRACT.mdセクション20.5)。
 * statusAndBody()を切り出しているのは、実サーバーを起動せずに単体テストできるようにするため
 * (backend-javaのRestErrorMapperと同じ動機)
 */
object RestErrorMapper {

    data class StatusAndBody(val status: Int, val body: ObjectNode)

    private val mapper = ObjectMapper()

    suspend fun write(call: ApplicationCall, error: TaskError) {
        val result = statusAndBody(error)
        call.respondText(
            text = mapper.writeValueAsString(result.body),
            contentType = ContentType.Application.Json,
            status = HttpStatusCode.fromValue(result.status),
        )
    }

    fun statusAndBody(error: TaskError): StatusAndBody {
        val body = mapper.createObjectNode()
        val status: Int
        when (error.kind) {
            TaskError.Kind.UNAUTHORIZED -> {
                status = 401
                body.put("error", "unauthorized")
            }
            TaskError.Kind.USER_NOT_PROVISIONED -> {
                status = 403
                body.put("error", "user_not_provisioned")
            }
            TaskError.Kind.INVALID_REQUEST -> {
                status = 400
                body.put("error", "invalid_request")
            }
            TaskError.Kind.INVALID_ID -> {
                status = 400
                body.put("error", "invalid_id")
            }
            TaskError.Kind.INVALID_STATUS -> {
                status = 422
                body.put("error", "invalid_status")
            }
            TaskError.Kind.INVALID_FINISHED_ON -> {
                status = 422
                body.put("error", "invalid_finished_on")
            }
            TaskError.Kind.VALIDATION -> {
                status = 422
                body.put("error", "validation_error")
                body.put("message", error.message)
            }
            TaskError.Kind.NOT_FOUND -> {
                status = 404
                body.put("error", "not_found")
            }
            TaskError.Kind.INTERNAL -> {
                status = 500
                body.put("error", "internal_server_error")
            }
        }
        return StatusAndBody(status, body)
    }
}
