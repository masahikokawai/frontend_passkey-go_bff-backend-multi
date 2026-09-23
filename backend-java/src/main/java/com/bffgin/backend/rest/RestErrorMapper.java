package com.bffgin.backend.rest;

import com.bffgin.backend.domain.TaskError;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.javalin.http.Context;

/**
 * backend-rustのerror.rs(RestError -> IntoResponse)と1文字も変えていないJSON形状・HTTPステータス。
 * ワイヤー契約パリティの原則(CONTRACT.mdセクション20.5)。
 * statusAndBody()を切り出しているのは、Javalinの実サーバーを起動せずに単体テストできるようにするため
 * (backend-rustのerror.rsが同じ理由でIntoResponseを直接テストしているのと同じ動機)
 */
public final class RestErrorMapper {

    public record StatusAndBody(int status, ObjectNode body) {
    }

    private static final ObjectMapper MAPPER = new ObjectMapper();

    private RestErrorMapper() {
    }

    public static void write(Context ctx, TaskError error) {
        StatusAndBody result = statusAndBody(error);
        ctx.status(result.status()).json(result.body());
    }

    public static StatusAndBody statusAndBody(TaskError error) {
        ObjectNode body = MAPPER.createObjectNode();
        int status;
        switch (error.kind()) {
            case UNAUTHORIZED -> {
                status = 401;
                body.put("error", "unauthorized");
            }
            case USER_NOT_PROVISIONED -> {
                status = 403;
                body.put("error", "user_not_provisioned");
            }
            case INVALID_REQUEST -> {
                status = 400;
                body.put("error", "invalid_request");
            }
            case INVALID_ID -> {
                status = 400;
                body.put("error", "invalid_id");
            }
            case INVALID_STATUS -> {
                status = 422;
                body.put("error", "invalid_status");
            }
            case INVALID_FINISHED_ON -> {
                status = 422;
                body.put("error", "invalid_finished_on");
            }
            case VALIDATION -> {
                status = 422;
                body.put("error", "validation_error");
                body.put("message", error.getMessage());
            }
            case NOT_FOUND -> {
                status = 404;
                body.put("error", "not_found");
            }
            case USER_ID_REQUIRED -> {
                status = 400;
                body.put("error", "user_id_required");
            }
            case INVALID_USER_ID -> {
                status = 400;
                body.put("error", "invalid_user_id");
            }
            default -> {
                status = 500;
                body.put("error", "internal_server_error");
            }
        }
        return new StatusAndBody(status, body);
    }
}
