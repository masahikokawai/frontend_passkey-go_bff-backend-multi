package com.bffgin.backend.grpc

import com.bffgin.backend.domain.Label
import com.bffgin.backend.domain.Task
import com.bffgin.backend.domain.TaskError
import com.bffgin.backend.domain.TaskInput
import com.bffgin.backend.proto.CreateTaskRequest
import com.bffgin.backend.proto.UpdateTaskRequest
import com.google.protobuf.Timestamp
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneOffset
import java.time.format.DateTimeParseException

internal object TaskProtoMapper {

    fun toProto(task: Task): com.bffgin.backend.proto.Task {
        val builder = com.bffgin.backend.proto.Task.newBuilder()
            .setId(task.id)
            .setName(task.name)
            .setStatus(task.status.wireValue)
            .setFinishedOn(task.finishedOn.toString())
            .setCreatedAt(toTimestamp(task.createdAt))
            .setUpdatedAt(toTimestamp(task.updatedAt))
        if (task.description != null) {
            builder.description = task.description
        }
        for (label: Label in task.labels) {
            builder.addLabels(
                com.bffgin.backend.proto.Label.newBuilder().setId(label.id).setName(label.name).build(),
            )
        }
        return builder.build()
    }

    fun fromCreateRequest(req: CreateTaskRequest): TaskInput {
        val finishedOn = parseFinishedOn(req.finishedOn)
        val description = if (req.hasDescription()) req.description else null
        return TaskInput(req.name, description, req.status, finishedOn, req.labelIdsList)
    }

    fun fromUpdateRequest(req: UpdateTaskRequest): TaskInput {
        val finishedOn = parseFinishedOn(req.finishedOn)
        val description = if (req.hasDescription()) req.description else null
        return TaskInput(req.name, description, req.status, finishedOn, req.labelIdsList)
    }

    private fun parseFinishedOn(raw: String): LocalDate {
        return try {
            LocalDate.parse(raw)
        } catch (e: DateTimeParseException) {
            throw TaskError.invalidFinishedOn()
        }
    }

    private fun toTimestamp(dt: LocalDateTime): Timestamp {
        val instant = dt.toInstant(ZoneOffset.UTC)
        return Timestamp.newBuilder().setSeconds(instant.epochSecond).setNanos(instant.nano).build()
    }
}
