package com.bffgin.backend.rest;

import com.bffgin.backend.auth.UserResolver;
import com.bffgin.backend.domain.Task;
import com.bffgin.backend.domain.TaskError;
import com.bffgin.backend.domain.TaskInput;
import com.bffgin.backend.domain.TaskStatus;
import com.bffgin.backend.domain.TaskValidation;
import com.bffgin.backend.repository.TaskRepository;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.javalin.http.Context;
import io.javalin.http.Handler;

import java.time.LocalDate;
import java.time.ZoneOffset;
import java.time.format.DateTimeParseException;
import java.util.ArrayList;
import java.util.List;

/**
 * 内部REST v1(:8111配下)。バリデーション・エラー形状はbackend-rustのrest/task.rs・error.rsと
 * 1文字も変えていない(CONTRACT.mdセクション20.5のワイヤー契約パリティ)
 */
public final class TaskRestHandler {

    private final TaskRepository repository;
    private final UserResolver userResolver;
    private final ObjectMapper mapper = new ObjectMapper();

    public final Handler list;
    public final Handler get;
    public final Handler create;
    public final Handler update;
    public final Handler delete;

    public TaskRestHandler(TaskRepository repository, UserResolver userResolver) {
        this.repository = repository;
        this.userResolver = userResolver;

        this.list = ctx -> {
            long userId = authenticate(ctx);
            int limit = ctx.queryParamAsClass("limit", Integer.class).getOrDefault(20);
            int offset = ctx.queryParamAsClass("offset", Integer.class).getOrDefault(0);

            var page = this.repository.listOffset(userId, limit, offset);
            ObjectNode body = mapper.createObjectNode();
            var tasksNode = body.putArray("tasks");
            for (Task t : page.tasks()) {
                tasksNode.add(TaskJson.toJson(t));
            }
            body.put("total", page.total());
            body.put("limit", limit);
            body.put("offset", offset);
            ctx.status(200).json(body);
        };

        this.get = ctx -> {
            long userId = authenticate(ctx);
            long id = parseId(ctx);
            Task task = this.repository.findById(id, userId).orElseThrow(TaskError::notFound);
            ctx.status(200).json(TaskJson.toJson(task));
        };

        this.create = ctx -> {
            long userId = authenticate(ctx);
            TaskInput input = parseBody(ctx);
            LocalDate today = LocalDate.now(ZoneOffset.UTC);
            TaskStatus status = TaskValidation.validate(input, today);

            long id = this.repository.create(userId, input, status);
            Task task = this.repository.findById(id, userId)
                    .orElseThrow(() -> TaskError.internal("task disappeared after create"));
            ctx.status(201).json(TaskJson.toJson(task));
        };

        this.update = ctx -> {
            long userId = authenticate(ctx);
            long id = parseId(ctx);
            TaskInput input = parseBody(ctx);
            LocalDate today = LocalDate.now(ZoneOffset.UTC);
            TaskStatus status = TaskValidation.validate(input, today);

            boolean updated = this.repository.update(id, userId, input, status);
            if (!updated) {
                throw TaskError.notFound();
            }
            Task task = this.repository.findById(id, userId).orElseThrow(TaskError::notFound);
            ctx.status(200).json(TaskJson.toJson(task));
        };

        this.delete = ctx -> {
            long userId = authenticate(ctx);
            long id = parseId(ctx);
            boolean deleted = this.repository.delete(id, userId);
            if (!deleted) {
                throw TaskError.notFound();
            }
            ctx.status(204);
        };
    }

    private long authenticate(Context ctx) throws TaskError {
        return userResolver.resolve(ctx.header("Authorization"));
    }

    private static long parseId(Context ctx) throws TaskError {
        try {
            return Long.parseLong(ctx.pathParam("id"));
        } catch (NumberFormatException e) {
            throw TaskError.invalidId();
        }
    }

    /** backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い */
    private TaskInput parseBody(Context ctx) throws TaskError {
        JsonNode body;
        try {
            body = mapper.readTree(ctx.body());
        } catch (Exception e) {
            throw TaskError.invalidRequest();
        }
        if (body == null || body.isNull()) {
            throw TaskError.invalidRequest();
        }
        String name = body.path("name").asText("");
        String status = body.path("status").asText("");
        String finishedOnRaw = body.path("finished_on").asText("");
        if (name.isEmpty() || status.isEmpty() || finishedOnRaw.isEmpty()) {
            throw TaskError.invalidRequest();
        }
        LocalDate finishedOn;
        try {
            finishedOn = LocalDate.parse(finishedOnRaw);
        } catch (DateTimeParseException e) {
            throw TaskError.invalidFinishedOn();
        }
        String description = body.hasNonNull("description") ? body.get("description").asText() : null;
        List<Long> labelIds = new ArrayList<>();
        if (body.has("label_ids") && body.get("label_ids").isArray()) {
            for (JsonNode idNode : body.get("label_ids")) {
                if (idNode.isIntegralNumber()) {
                    labelIds.add(idNode.asLong());
                }
            }
        }
        return new TaskInput(name, description, status, finishedOn, labelIds);
    }
}
