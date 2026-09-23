package com.bffgin.backend.rest;

import com.bffgin.backend.domain.Task;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;

import java.time.format.DateTimeFormatter;

/**
 * CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
 * 【backend(Go)の実際の挙動に合わせた既知の差異】taskDTOToJSON(backend/internal/handler/v1/task.go)は
 * user_idをレスポンスに含めていない(CONTRACT.md本文の例には書かれているが、実装はそうなっていない。
 * ワイヤー契約パリティの原則(セクション20.5)に従い、ドキュメントではなく実際の挙動に合わせる。
 * backend-rust/backend-c/backend-cppの全てで同じ既知の差異が確認・踏襲されている)
 */
public final class TaskJson {

    private static final ObjectMapper MAPPER = new ObjectMapper();
    private static final DateTimeFormatter RFC3339 = DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss'+00:00'");

    private TaskJson() {
    }

    /** 内部REST v1・外部公開APIの両方で共有する(user_idを含まない同じ形状、TaskJson参照) */
    public static ObjectNode toJson(Task task) {
        ObjectNode node = MAPPER.createObjectNode();
        node.put("id", task.id());
        node.put("name", task.name());
        if (task.description() == null) {
            node.putNull("description");
        } else {
            node.put("description", task.description());
        }
        node.put("status", task.status().wireValue());
        node.put("finished_on", task.finishedOn().toString());
        ArrayNode labels = node.putArray("labels");
        for (var label : task.labels()) {
            ObjectNode labelNode = MAPPER.createObjectNode();
            labelNode.put("id", label.id());
            labelNode.put("name", label.name());
            labels.add(labelNode);
        }
        node.put("created_at", task.createdAt().format(RFC3339));
        node.put("updated_at", task.updatedAt().format(RFC3339));
        return node;
    }
}
