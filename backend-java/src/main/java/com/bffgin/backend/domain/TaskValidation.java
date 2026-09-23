package com.bffgin.backend.domain;

import java.time.LocalDate;

/**
 * backend(Go)のservice.validateTaskInputと同じルール:
 *   - name: 必須・20コードポイント以内
 *   - finished_on: 過去日不可(UTC基準の「今日」)
 *   - status: enumの範囲内
 *
 * 【他言語での既知の落とし穴】name の文字数判定は「コードポイント数」で行うこと。
 * JavaのString#lengthはUTF-16コード単位数を返すため、基本多言語面外の文字(絵文字等)を
 * 含む名前ではサロゲートペア1文字が2としてカウントされ、契約違反になる
 * (backend-scala-http4sで実際に見つかった不一致、backend-rustのmodel.rsのコメント参照)。
 * ここでは String#codePointCount を使う
 */
public final class TaskValidation {

    private TaskValidation() {
    }

    public static TaskStatus validate(TaskInput input, LocalDate today) throws TaskError {
        if (input.name() == null || input.name().isEmpty()) {
            throw TaskError.validation("nameは必須です");
        }
        int codePointCount = input.name().codePointCount(0, input.name().length());
        if (codePointCount > 20) {
            throw TaskError.validation("nameは20文字以内である必要があります");
        }
        if (input.finishedOn() == null || input.finishedOn().isBefore(today)) {
            throw TaskError.validation("finished_onに過去日は指定できません");
        }
        return TaskStatus.fromWireValue(input.statusRaw())
                .orElseThrow(() -> TaskError.validation("不明なstatus: \"" + input.statusRaw() + "\""));
    }
}
