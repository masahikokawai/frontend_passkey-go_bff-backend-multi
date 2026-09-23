package com.bffgin.backend.grpc;

import com.bffgin.backend.domain.Label;
import com.bffgin.backend.domain.Task;
import com.bffgin.backend.domain.TaskError;
import com.bffgin.backend.domain.TaskInput;
import com.bffgin.backend.proto.CreateTaskRequest;
import com.bffgin.backend.proto.UpdateTaskRequest;
import com.google.protobuf.Timestamp;

import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.ZoneOffset;
import java.time.format.DateTimeParseException;
import java.util.List;

final class TaskProtoMapper {

    private TaskProtoMapper() {
    }

    static com.bffgin.backend.proto.Task toProto(Task task) {
        var builder = com.bffgin.backend.proto.Task.newBuilder()
                .setId(task.id())
                .setName(task.name())
                .setStatus(task.status().wireValue())
                .setFinishedOn(task.finishedOn().toString())
                .setCreatedAt(toTimestamp(task.createdAt()))
                .setUpdatedAt(toTimestamp(task.updatedAt()));
        if (task.description() != null) {
            builder.setDescription(task.description());
        }
        for (Label label : task.labels()) {
            builder.addLabels(com.bffgin.backend.proto.Label.newBuilder()
                    .setId(label.id())
                    .setName(label.name())
                    .build());
        }
        return builder.build();
    }

    static TaskInput fromCreateRequest(CreateTaskRequest req) throws TaskError {
        LocalDate finishedOn = parseFinishedOn(req.getFinishedOn());
        String description = req.hasDescription() ? req.getDescription() : null;
        return new TaskInput(req.getName(), description, req.getStatus(), finishedOn, req.getLabelIdsList());
    }

    static TaskInput fromUpdateRequest(UpdateTaskRequest req) throws TaskError {
        LocalDate finishedOn = parseFinishedOn(req.getFinishedOn());
        String description = req.hasDescription() ? req.getDescription() : null;
        return new TaskInput(req.getName(), description, req.getStatus(), finishedOn, req.getLabelIdsList());
    }

    private static LocalDate parseFinishedOn(String raw) throws TaskError {
        try {
            return LocalDate.parse(raw);
        } catch (DateTimeParseException e) {
            throw TaskError.invalidFinishedOn();
        }
    }

    private static Timestamp toTimestamp(LocalDateTime dt) {
        var instant = dt.toInstant(ZoneOffset.UTC);
        return Timestamp.newBuilder().setSeconds(instant.getEpochSecond()).setNanos(instant.getNano()).build();
    }
}
