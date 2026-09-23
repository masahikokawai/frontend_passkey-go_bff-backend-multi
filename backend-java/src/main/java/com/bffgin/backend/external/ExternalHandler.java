package com.bffgin.backend.external;

import com.bffgin.backend.auth.Dispatcher;
import com.bffgin.backend.auth.ExternalAuth;
import com.bffgin.backend.domain.Task;
import com.bffgin.backend.flags.FeatureFlagPoller;
import com.bffgin.backend.repository.TaskRepository;
import com.bffgin.backend.rest.TaskJson;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.javalin.http.Handler;

import java.util.HashMap;
import java.util.Map;

/**
 * 外部公開API(:8112配下、CONTRACT.mdセクション11)。bffを経由しない、
 * Client Credentials Grantのみを受け付ける唯一のエンドポイント。
 * offset(v1、既定)/cursor(v2、backend.external-tasks-pagination-v2がON)の
 * 2つのページング方式を切り替える(内部REST v1・gRPC v2とは別のFeature Flag軸)
 */
public final class ExternalHandler {

    private final TaskRepository repository;
    private final Dispatcher dispatcher;
    private final FeatureFlagPoller flagPoller;
    private final String externalApiClientId;
    private final ObjectMapper mapper = new ObjectMapper();

    public final Handler list;

    public ExternalHandler(TaskRepository repository, Dispatcher dispatcher, FeatureFlagPoller flagPoller,
            String externalApiClientId) {
        this.repository = repository;
        this.dispatcher = dispatcher;
        this.flagPoller = flagPoller;
        this.externalApiClientId = externalApiClientId;

        this.list = ctx -> {
            ExternalAuth.requireExternalClient(dispatcher, ctx.header("Authorization"), this.externalApiClientId);

            Map<String, String> params = new HashMap<>();
            ctx.queryParamMap().forEach((k, v) -> params.put(k, v.isEmpty() ? "" : v.get(0)));
            long userId = ExternalQuery.parseUserId(params);

            boolean useV2 = "on".equals(flagPoller.variation("backend.external-tasks-pagination-v2", "off"));

            ObjectNode body = mapper.createObjectNode();
            if (useV2) {
                ExternalQuery.CursorParams cp = ExternalQuery.parseCursorParams(params);
                var tasks = repository.listCursor(userId, cp.afterId(), cp.limit());
                var tasksNode = body.putArray("tasks");
                long lastId = 0;
                for (Task t : tasks) {
                    tasksNode.add(TaskJson.toJson(t));
                    lastId = t.id();
                }
                var nextCursor = ExternalQuery.nextCursor(lastId);
                if (nextCursor.isPresent()) {
                    body.put("next_cursor", nextCursor.get());
                } else {
                    body.putNull("next_cursor");
                }
                body.put("limit", cp.limit());
            } else {
                ExternalQuery.OffsetParams op = ExternalQuery.parseOffsetParams(params);
                int offset = (op.page() - 1) * op.pageSize();
                var page = repository.listOffset(userId, op.pageSize(), offset);
                var tasksNode = body.putArray("tasks");
                for (Task t : page.tasks()) {
                    tasksNode.add(TaskJson.toJson(t));
                }
                body.put("page", op.page());
                body.put("page_size", op.pageSize());
                body.put("total", page.total());
            }
            ctx.status(200).json(body);
        };
    }
}
