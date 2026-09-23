package com.bffgin.backend.rest

/**
 * 外部公開API専用のエラー種別。内部REST v1のTaskError(unauthorized/not_found等)とは
 * JSON形状・意味が異なる(例: 認証失敗は"unauthenticated"、内部は"unauthorized")ため、
 * 混同を避けて別クラスにする(backend-c/backend-cppのRequireExternalClientAuth周りの
 * エラー方針と同じ)
 */
class ExternalApiError private constructor(val kind: Kind, message: String) : Exception(message) {

    enum class Kind {
        UNAUTHENTICATED,
        USER_ID_REQUIRED,
        INVALID_USER_ID,
    }

    companion object {
        fun unauthenticated() = ExternalApiError(Kind.UNAUTHENTICATED, "unauthenticated")
        fun userIdRequired() = ExternalApiError(Kind.USER_ID_REQUIRED, "user_id_required")
        fun invalidUserId() = ExternalApiError(Kind.INVALID_USER_ID, "invalid_user_id")
    }
}
