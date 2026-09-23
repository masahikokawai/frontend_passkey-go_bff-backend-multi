package com.bffgin.backend.domain

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.time.LocalDate

/**
 * backend-java/backend-rustのTaskValidationTest/model.rs(mod tests)と同じ境界値・異常系を踏襲する。
 * 特にnameのコードポイント数判定(String.lengthのUTF-16コード単位数との違い)は
 * backend-scala-http4s/backend-javaで実際に見つかった不一致があるため重点的に確認する
 */
class TaskValidationTest {

    private fun validInput(today: LocalDate) = TaskInput("buy milk", null, "waiting", today, emptyList())

    @Test
    fun acceptsValidInput() {
        val today = LocalDate.of(2026, 9, 9)
        val status = TaskValidation.validate(validInput(today), today)
        assertEquals(TaskStatus.WAITING, status)
    }

    @Test
    fun rejectsEmptyName() {
        val today = LocalDate.of(2026, 9, 9)
        val input = TaskInput("", null, "waiting", today, emptyList())
        val err = assertFailsWithTaskError { TaskValidation.validate(input, today) }
        assertEquals(TaskError.Kind.VALIDATION, err.kind)
    }

    @Test
    fun rejectsNameOver20Codepoints() {
        val today = LocalDate.of(2026, 9, 9)
        val input = TaskInput("a".repeat(21), null, "waiting", today, emptyList())
        val err = assertFailsWithTaskError { TaskValidation.validate(input, today) }
        assertEquals(TaskError.Kind.VALIDATION, err.kind)
    }

    @Test
    fun acceptsNameExactly20Codepoints() {
        val today = LocalDate.of(2026, 9, 9)
        val input = TaskInput("a".repeat(20), null, "waiting", today, emptyList())
        assertEquals(TaskStatus.WAITING, TaskValidation.validate(input, today))
    }

    /**
     * 【他言語で見つかった既知の落とし穴】絵文字(基本多言語面外、サロゲートペア)を含む名前で
     * コードポイント数とUTF-16コード単位数がずれる境界値。20コードポイントの絵文字名は
     * String.lengthでは40(2倍)になるが、正しくは受理されるべき
     */
    @Test
    fun acceptsAstralEmojiNameOf20Codepoints() {
        val today = LocalDate.of(2026, 9, 9)
        val emojiName = "😀".repeat(20) // 😀 x20、コードポイント数20、UTF-16コード単位数40
        val input = TaskInput(emojiName, null, "waiting", today, emptyList())
        assertEquals(TaskStatus.WAITING, TaskValidation.validate(input, today))
    }

    @Test
    fun rejectsAstralEmojiNameOf21Codepoints() {
        val today = LocalDate.of(2026, 9, 9)
        val emojiName = "😀".repeat(21)
        val input = TaskInput(emojiName, null, "waiting", today, emptyList())
        val err = assertFailsWithTaskError { TaskValidation.validate(input, today) }
        assertEquals(TaskError.Kind.VALIDATION, err.kind)
    }

    @Test
    fun rejectsPastFinishedOn() {
        val today = LocalDate.of(2026, 9, 9)
        val input = TaskInput("x", null, "waiting", today.minusDays(1), emptyList())
        val err = assertFailsWithTaskError { TaskValidation.validate(input, today) }
        assertEquals(TaskError.Kind.VALIDATION, err.kind)
    }

    @Test
    fun acceptsFinishedOnEqualToToday() {
        val today = LocalDate.of(2026, 9, 9)
        val input = TaskInput("x", null, "waiting", today, emptyList())
        assertEquals(TaskStatus.WAITING, TaskValidation.validate(input, today))
    }

    @Test
    fun rejectsUnknownStatus() {
        val today = LocalDate.of(2026, 9, 9)
        val input = TaskInput("x", null, "not_a_status", today, emptyList())
        val err = assertFailsWithTaskError { TaskValidation.validate(input, today) }
        assertEquals(TaskError.Kind.VALIDATION, err.kind)
    }

    @Test
    fun statusRoundTripsThroughWireAndDbValue() {
        for (status in TaskStatus.entries) {
            assertEquals(status, TaskStatus.fromWireValue(status.wireValue))
            assertEquals(status, TaskStatus.fromDbValue(status.dbValue))
        }
        assertNull(TaskStatus.fromWireValue("bogus"))
        assertNull(TaskStatus.fromDbValue(0))
        assertNull(TaskStatus.fromDbValue(99))
    }

    private fun assertFailsWithTaskError(block: () -> Unit): TaskError {
        try {
            block()
        } catch (e: TaskError) {
            return e
        }
        throw AssertionError("Expected TaskError to be thrown, but nothing was thrown")
    }
}
