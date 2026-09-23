package com.bffgin.backend.rest

/**
 * 外部公開API(GET /external/v1/tasks)のクエリパラメータ解析・ページング計算。
 * 実サーバー無しで単体テストできるように、純粋関数として切り出す
 * (backend-c/external_query.{h,c}・backend-cppの同等ロジックと同じ分離方針)
 */
object ExternalQuery {

    data class OffsetParams(val page: Int, val pageSize: Int)
    data class CursorParams(val cursor: Long, val limit: Int)

    /** user_idクエリパラメータの必須チェック+数値変換 */
    fun parseUserId(raw: String?): Long {
        if (raw.isNullOrEmpty()) {
            throw ExternalApiError.userIdRequired()
        }
        return raw.toLongOrNull() ?: throw ExternalApiError.invalidUserId()
    }

    /** page既定1・page_size既定10、いずれも1未満は1に補正する(負数・0のpage/page_sizeを弾く) */
    fun parseOffsetParams(pageRaw: String?, pageSizeRaw: String?): OffsetParams {
        val page = (pageRaw?.toIntOrNull() ?: 1).coerceAtLeast(1)
        val pageSize = (pageSizeRaw?.toIntOrNull() ?: 10).coerceAtLeast(1)
        return OffsetParams(page, pageSize)
    }

    /** cursor省略時は0(先頭から)、limit既定10・1未満は1に補正 */
    fun parseCursorParams(cursorRaw: String?, limitRaw: String?): CursorParams {
        val cursor = cursorRaw?.toLongOrNull()?.takeIf { it > 0 } ?: 0L
        val limit = (limitRaw?.toIntOrNull() ?: 10).coerceAtLeast(1)
        return CursorParams(cursor, limit)
    }

    fun offsetToLimitOffset(params: OffsetParams): Pair<Int, Int> {
        val limit = params.pageSize
        val offset = (params.page - 1) * params.pageSize
        return limit to offset
    }
}
