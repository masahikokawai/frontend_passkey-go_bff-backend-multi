package com.bffgin.backend.domain

/**
 * backend-java/backend-rustのTaskError/RestErrorと同じ分類を1つのクラスにまとめたもの。
 * JSON形状・HTTPステータス・gRPCステータスへの変換はトランスポート層(rest/grpc)がそれぞれ担う。
 * Kotlinには検査例外(checked exception)が無いため、backend-javaのように呼び出し元に
 * `throws TaskError`の宣言を強制することはできないが、suspend fun群は全てこの例外だけを
 * 投げる設計を維持している
 */
class TaskError private constructor(val kind: Kind, message: String) : Exception(message) {

    enum class Kind {
        UNAUTHORIZED,
        USER_NOT_PROVISIONED,
        INVALID_REQUEST,
        INVALID_ID,
        INVALID_STATUS,
        INVALID_FINISHED_ON,
        VALIDATION,
        NOT_FOUND,
        INTERNAL,
    }

    companion object {
        fun unauthorized() = TaskError(Kind.UNAUTHORIZED, "unauthorized")
        fun userNotProvisioned() = TaskError(Kind.USER_NOT_PROVISIONED, "user_not_provisioned")
        fun invalidRequest() = TaskError(Kind.INVALID_REQUEST, "invalid_request")
        fun invalidId() = TaskError(Kind.INVALID_ID, "invalid_id")
        fun invalidStatus() = TaskError(Kind.INVALID_STATUS, "invalid_status")
        fun invalidFinishedOn() = TaskError(Kind.INVALID_FINISHED_ON, "invalid_finished_on")
        fun validation(message: String) = TaskError(Kind.VALIDATION, message)
        fun notFound() = TaskError(Kind.NOT_FOUND, "not_found")
        fun internal(message: String) = TaskError(Kind.INTERNAL, message)
    }
}
