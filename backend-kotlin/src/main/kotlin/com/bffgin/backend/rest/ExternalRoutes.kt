package com.bffgin.backend.rest

import com.bffgin.backend.auth.Dispatcher
import com.bffgin.backend.auth.ExternalAuth
import com.bffgin.backend.flags.FeatureFlagPoller
import com.bffgin.backend.repository.TaskRepository
import com.fasterxml.jackson.databind.ObjectMapper
import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import io.ktor.server.application.ApplicationCall
import io.ktor.server.application.call
import io.ktor.server.response.respondText
import io.ktor.server.routing.Route
import io.ktor.server.routing.get

/**
 * 外部公開API(:8114配下、CONTRACT.mdセクション11)。BFFを経由しない唯一の例外的な経路。
 * backend.external-tasks-pagination-v2(Feature Flag)でoffset(v1、既定)/cursor(v2)を切り替える。
 * 内部REST v1のtaskRoutesと同じくsuspend funで貫かれている
 */
private val mapper = ObjectMapper()

fun Route.externalRoutes(
    repository: TaskRepository,
    dispatcher: Dispatcher,
    flagPoller: FeatureFlagPoller,
    externalApiClientId: String,
) {
    get("/external/v1/tasks") {
        try {
            ExternalAuth.requireExternalClient(call.request.headers["Authorization"], dispatcher, externalApiClientId)

            val userId = ExternalQuery.parseUserId(call.request.queryParameters["user_id"])
            val useV2 = flagPoller.variation("backend.external-tasks-pagination-v2", "off") == "on"

            if (useV2) {
                respondCursorPage(call, repository, userId)
            } else {
                respondOffsetPage(call, repository, userId)
            }
        } catch (e: ExternalApiError) {
            respondError(call, e)
        }
    }
}

private suspend fun respondOffsetPage(call: ApplicationCall, repository: TaskRepository, userId: Long) {
    val params = ExternalQuery.parseOffsetParams(
        call.request.queryParameters["page"],
        call.request.queryParameters["page_size"],
    )
    val (limit, offset) = ExternalQuery.offsetToLimitOffset(params)
    val page = repository.listOffset(userId, limit, offset)

    val body = mapper.createObjectNode()
    val tasksNode = body.putArray("tasks")
    for (t in page.tasks) {
        tasksNode.add(TaskJson.toJson(t))
    }
    body.put("page", params.page)
    body.put("page_size", params.pageSize)
    body.put("total", page.total)
    call.respondText(mapper.writeValueAsString(body), ContentType.Application.Json, HttpStatusCode.OK)
}

private suspend fun respondCursorPage(call: ApplicationCall, repository: TaskRepository, userId: Long) {
    val params = ExternalQuery.parseCursorParams(
        call.request.queryParameters["cursor"],
        call.request.queryParameters["limit"],
    )
    // limitぶんだけ取得する(次ページ判定は「最後の行のidをそのまま次回のcursorとして返す」
    // という簡略設計、backend-c/backend-cppの「cursorは最後の行の生のid」という規約と同じ)
    val tasks = repository.listCursor(userId, params.cursor, params.limit)

    val body = mapper.createObjectNode()
    val tasksNode = body.putArray("tasks")
    for (t in tasks) {
        tasksNode.add(TaskJson.toJson(t))
    }
    val nextCursor = tasks.lastOrNull()?.id
    if (nextCursor == null) {
        body.putNull("next_cursor")
    } else {
        body.put("next_cursor", nextCursor.toString())
    }
    body.put("limit", params.limit)
    call.respondText(mapper.writeValueAsString(body), ContentType.Application.Json, HttpStatusCode.OK)
}

private suspend fun respondError(call: ApplicationCall, error: ExternalApiError) {
    val body = mapper.createObjectNode()
    val status: Int
    when (error.kind) {
        ExternalApiError.Kind.UNAUTHENTICATED -> {
            status = 401
            body.put("error", "unauthenticated")
        }
        ExternalApiError.Kind.USER_ID_REQUIRED -> {
            status = 400
            body.put("error", "user_id_required")
        }
        ExternalApiError.Kind.INVALID_USER_ID -> {
            status = 400
            body.put("error", "invalid_user_id")
        }
    }
    call.respondText(mapper.writeValueAsString(body), ContentType.Application.Json, HttpStatusCode.fromValue(status))
}
