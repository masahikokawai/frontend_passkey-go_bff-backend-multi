package com.bffgin.backend.domain;

import java.time.LocalDate;
import java.time.LocalDateTime;
import java.util.List;

/**
 * user_idはワイヤーに乗せない(REST/gRPCとも)。backend(Go)実装が
 * taskDTOToJSONでuser_idを含めていない実際の挙動に合わせている
 * (CONTRACT.mdセクション5.1本文の例には書かれているが、ワイヤー契約パリティの原則
 * (セクション20.5)により、ドキュメントではなく実際の挙動に合わせる。backend-rustの
 * rest/task.rsのコメント参照)
 */
public record Task(
        long id,
        String name,
        String description,
        TaskStatus status,
        LocalDate finishedOn,
        long userId,
        List<Label> labels,
        LocalDateTime createdAt,
        LocalDateTime updatedAt) {
}
