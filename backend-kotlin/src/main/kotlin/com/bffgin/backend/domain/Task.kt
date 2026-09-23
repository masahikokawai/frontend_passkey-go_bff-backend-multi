package com.bffgin.backend.domain

import java.time.LocalDate
import java.time.LocalDateTime

/**
 * user_idはワイヤーに乗せない(REST/gRPCとも)。backend(Go)実装が
 * taskDTOToJSONでuser_idを含めていない実際の挙動に合わせている
 * (CONTRACT.mdセクション5.1本文の例には書かれているが、ワイヤー契約パリティの原則
 * (セクション20.5)により、ドキュメントではなく実際の挙動に合わせる。backend-javaと同じ既知の差異)
 */
data class Task(
    val id: Long,
    val name: String,
    val description: String?,
    val status: TaskStatus,
    val finishedOn: LocalDate,
    val userId: Long,
    val labels: List<Label>,
    val createdAt: LocalDateTime,
    val updatedAt: LocalDateTime,
)
