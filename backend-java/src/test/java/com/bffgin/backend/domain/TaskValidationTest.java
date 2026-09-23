package com.bffgin.backend.domain;

import org.junit.jupiter.api.Test;

import java.time.LocalDate;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

/**
 * backend-rustのsrc/model.rs(mod tests)と同じ境界値・異常系を踏襲する。
 * 特にnameのコードポイント数判定(String#lengthのUTF-16コード単位数との違い)は
 * backend-scala-http4sで実際に見つかった不一致があるため重点的に確認する
 */
class TaskValidationTest {

    private static TaskInput validInput(LocalDate today) {
        return new TaskInput("buy milk", null, "waiting", today, List.of());
    }

    @Test
    void acceptsValidInput() throws TaskError {
        LocalDate today = LocalDate.of(2026, 9, 9);
        TaskStatus status = TaskValidation.validate(validInput(today), today);
        assertEquals(TaskStatus.WAITING, status);
    }

    @Test
    void rejectsEmptyName() {
        LocalDate today = LocalDate.of(2026, 9, 9);
        TaskInput input = new TaskInput("", null, "waiting", today, List.of());
        TaskError err = assertThrows(TaskError.class, () -> TaskValidation.validate(input, today));
        assertEquals(TaskError.Kind.VALIDATION, err.kind());
    }

    @Test
    void rejectsNameOver20Codepoints() {
        LocalDate today = LocalDate.of(2026, 9, 9);
        TaskInput input = new TaskInput("a".repeat(21), null, "waiting", today, List.of());
        TaskError err = assertThrows(TaskError.class, () -> TaskValidation.validate(input, today));
        assertEquals(TaskError.Kind.VALIDATION, err.kind());
    }

    @Test
    void acceptsNameExactly20Codepoints() throws TaskError {
        LocalDate today = LocalDate.of(2026, 9, 9);
        TaskInput input = new TaskInput("a".repeat(20), null, "waiting", today, List.of());
        assertEquals(TaskStatus.WAITING, TaskValidation.validate(input, today));
    }

    /**
     * 【他言語で見つかった既知の落とし穴】絵文字(基本多言語面外、サロゲートペア)を含む名前で
     * コードポイント数とUTF-16コード単位数がずれる境界値。20コードポイントの絵文字名は
     * String#length()では40(2倍)になるが、正しくは受理されるべき
     */
    @Test
    void acceptsAstralEmojiNameOf20Codepoints() throws TaskError {
        LocalDate today = LocalDate.of(2026, 9, 9);
        String emojiName = "😀".repeat(20); // 😀 x20、コードポイント数20、UTF-16コード単位数40
        TaskInput input = new TaskInput(emojiName, null, "waiting", today, List.of());
        assertEquals(TaskStatus.WAITING, TaskValidation.validate(input, today));
    }

    @Test
    void rejectsAstralEmojiNameOf21Codepoints() {
        LocalDate today = LocalDate.of(2026, 9, 9);
        String emojiName = "😀".repeat(21);
        TaskInput input = new TaskInput(emojiName, null, "waiting", today, List.of());
        TaskError err = assertThrows(TaskError.class, () -> TaskValidation.validate(input, today));
        assertEquals(TaskError.Kind.VALIDATION, err.kind());
    }

    @Test
    void rejectsPastFinishedOn() {
        LocalDate today = LocalDate.of(2026, 9, 9);
        TaskInput input = new TaskInput("x", null, "waiting", today.minusDays(1), List.of());
        TaskError err = assertThrows(TaskError.class, () -> TaskValidation.validate(input, today));
        assertEquals(TaskError.Kind.VALIDATION, err.kind());
    }

    @Test
    void acceptsFinishedOnEqualToToday() throws TaskError {
        LocalDate today = LocalDate.of(2026, 9, 9);
        TaskInput input = new TaskInput("x", null, "waiting", today, List.of());
        assertEquals(TaskStatus.WAITING, TaskValidation.validate(input, today));
    }

    @Test
    void rejectsUnknownStatus() {
        LocalDate today = LocalDate.of(2026, 9, 9);
        TaskInput input = new TaskInput("x", null, "not_a_status", today, List.of());
        TaskError err = assertThrows(TaskError.class, () -> TaskValidation.validate(input, today));
        assertEquals(TaskError.Kind.VALIDATION, err.kind());
    }

    @Test
    void statusRoundTripsThroughWireAndDbValue() {
        for (TaskStatus status : TaskStatus.values()) {
            assertEquals(status, TaskStatus.fromWireValue(status.wireValue()).orElseThrow());
            assertEquals(status, TaskStatus.fromDbValue(status.dbValue()).orElseThrow());
        }
        assertEquals(java.util.Optional.empty(), TaskStatus.fromWireValue("bogus"));
        assertEquals(java.util.Optional.empty(), TaskStatus.fromDbValue(0));
        assertEquals(java.util.Optional.empty(), TaskStatus.fromDbValue(99));
    }
}
