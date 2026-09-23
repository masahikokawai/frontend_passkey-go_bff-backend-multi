package com.bffgin.backend.rest

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

/** ExternalQueryの純粋関数を実サーバー無しで検証する(backend-c/backend-cppのexternal_query_test相当) */
class ExternalQueryTest {

    @Test
    fun parseUserIdAcceptsValidValue() {
        assertEquals(42L, ExternalQuery.parseUserId("42"))
    }

    @Test
    fun parseUserIdRejectsMissing() {
        val e = assertThrows(ExternalApiError::class.java) { ExternalQuery.parseUserId(null) }
        assertEquals(ExternalApiError.Kind.USER_ID_REQUIRED, e.kind)
    }

    @Test
    fun parseUserIdRejectsEmpty() {
        val e = assertThrows(ExternalApiError::class.java) { ExternalQuery.parseUserId("") }
        assertEquals(ExternalApiError.Kind.USER_ID_REQUIRED, e.kind)
    }

    @Test
    fun parseUserIdRejectsNonNumeric() {
        val e = assertThrows(ExternalApiError::class.java) { ExternalQuery.parseUserId("abc") }
        assertEquals(ExternalApiError.Kind.INVALID_USER_ID, e.kind)
    }

    @Test
    fun parseOffsetParamsUsesDefaults() {
        val params = ExternalQuery.parseOffsetParams(null, null)
        assertEquals(1, params.page)
        assertEquals(10, params.pageSize)
    }

    @Test
    fun parseOffsetParamsClampsBelowOne() {
        val params = ExternalQuery.parseOffsetParams("0", "-5")
        assertEquals(1, params.page)
        assertEquals(1, params.pageSize)
    }

    @Test
    fun parseOffsetParamsHonorsExplicitValues() {
        val params = ExternalQuery.parseOffsetParams("3", "25")
        assertEquals(3, params.page)
        assertEquals(25, params.pageSize)
    }

    @Test
    fun offsetToLimitOffsetComputesCorrectly() {
        val (limit, offset) = ExternalQuery.offsetToLimitOffset(ExternalQuery.OffsetParams(page = 3, pageSize = 10))
        assertEquals(10, limit)
        assertEquals(20, offset)
    }

    @Test
    fun parseCursorParamsDefaultsToZeroAndTen() {
        val params = ExternalQuery.parseCursorParams(null, null)
        assertEquals(0L, params.cursor)
        assertEquals(10, params.limit)
    }

    @Test
    fun parseCursorParamsHonorsExplicitValues() {
        val params = ExternalQuery.parseCursorParams("123", "5")
        assertEquals(123L, params.cursor)
        assertEquals(5, params.limit)
    }

    @Test
    fun parseCursorParamsTreatsNonPositiveCursorAsUnset() {
        val params = ExternalQuery.parseCursorParams("0", "5")
        assertEquals(0L, params.cursor)
    }

    @Test
    fun parseCursorParamsClampsLimitBelowOne() {
        val params = ExternalQuery.parseCursorParams("5", "-1")
        assertEquals(1, params.limit)
    }
}
