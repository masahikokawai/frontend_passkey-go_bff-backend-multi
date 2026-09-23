package com.bffgin.backend.external;

import com.bffgin.backend.domain.TaskError;
import org.junit.jupiter.api.Test;

import java.util.Map;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

class ExternalQueryTest {

    @Test
    void parseUserIdReturnsValue() throws TaskError {
        assertEquals(42L, ExternalQuery.parseUserId(Map.of("user_id", "42")));
    }

    @Test
    void parseUserIdRejectsMissing() {
        TaskError e = assertThrows(TaskError.class, () -> ExternalQuery.parseUserId(Map.of()));
        assertEquals(TaskError.Kind.USER_ID_REQUIRED, e.kind());
    }

    @Test
    void parseUserIdRejectsEmpty() {
        TaskError e = assertThrows(TaskError.class, () -> ExternalQuery.parseUserId(Map.of("user_id", "")));
        assertEquals(TaskError.Kind.USER_ID_REQUIRED, e.kind());
    }

    @Test
    void parseUserIdRejectsNonNumeric() {
        TaskError e = assertThrows(TaskError.class, () -> ExternalQuery.parseUserId(Map.of("user_id", "abc")));
        assertEquals(TaskError.Kind.INVALID_USER_ID, e.kind());
    }

    @Test
    void offsetParamsDefaultToPage1Size10() {
        var params = ExternalQuery.parseOffsetParams(Map.of());
        assertEquals(1, params.page());
        assertEquals(10, params.pageSize());
    }

    @Test
    void offsetParamsParseProvidedValues() {
        var params = ExternalQuery.parseOffsetParams(Map.of("page", "3", "page_size", "5"));
        assertEquals(3, params.page());
        assertEquals(5, params.pageSize());
    }

    @Test
    void offsetParamsClampNonPositiveToOne() {
        var params = ExternalQuery.parseOffsetParams(Map.of("page", "0", "page_size", "-5"));
        assertEquals(1, params.page());
        assertEquals(1, params.pageSize());
    }

    @Test
    void offsetParamsFallBackToDefaultOnGarbage() {
        var params = ExternalQuery.parseOffsetParams(Map.of("page", "not-a-number"));
        assertEquals(1, params.page());
    }

    @Test
    void cursorParamsDefaultToStartWithLimit10() {
        var params = ExternalQuery.parseCursorParams(Map.of());
        assertEquals(0, params.afterId());
        assertEquals(10, params.limit());
    }

    @Test
    void cursorParamsParseProvidedValues() {
        var params = ExternalQuery.parseCursorParams(Map.of("cursor", "42", "limit", "3"));
        assertEquals(42L, params.afterId());
        assertEquals(3, params.limit());
    }

    @Test
    void cursorParamsTreatGarbageCursorAsStart() {
        var params = ExternalQuery.parseCursorParams(Map.of("cursor", "not-a-number"));
        assertEquals(0, params.afterId());
    }

    @Test
    void cursorParamsClampNonPositiveLimitToOne() {
        var params = ExternalQuery.parseCursorParams(Map.of("limit", "0"));
        assertEquals(1, params.limit());
    }

    @Test
    void nextCursorEmptyWhenLastIdZero() {
        assertEquals(Optional.empty(), ExternalQuery.nextCursor(0));
    }

    @Test
    void nextCursorPresentWhenLastIdPositive() {
        assertTrue(ExternalQuery.nextCursor(7).isPresent());
        assertEquals("7", ExternalQuery.nextCursor(7).get());
    }
}
