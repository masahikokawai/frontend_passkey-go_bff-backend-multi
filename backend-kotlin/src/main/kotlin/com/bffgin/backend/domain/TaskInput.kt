package com.bffgin.backend.domain

import java.time.LocalDate

data class TaskInput(
    val name: String,
    val description: String?,
    val statusRaw: String,
    val finishedOn: LocalDate?,
    val labelIds: List<Long>,
)
