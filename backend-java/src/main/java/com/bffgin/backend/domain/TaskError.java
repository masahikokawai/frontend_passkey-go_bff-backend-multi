package com.bffgin.backend.domain;

/**
 * backend-rustのRestError(error.rs)と同じ分類を1つのクラスにまとめたもの。
 * JSON形状・HTTPステータス・gRPCステータスへの変換はトランスポート層(rest/grpc)がそれぞれ担う
 */
public final class TaskError extends Exception {

    public enum Kind {
        UNAUTHORIZED,
        USER_NOT_PROVISIONED,
        INVALID_REQUEST,
        INVALID_ID,
        INVALID_STATUS,
        INVALID_FINISHED_ON,
        VALIDATION,
        NOT_FOUND,
        INTERNAL,
        USER_ID_REQUIRED,
        INVALID_USER_ID
    }

    private final Kind kind;

    private TaskError(Kind kind, String message) {
        super(message);
        this.kind = kind;
    }

    public Kind kind() {
        return kind;
    }

    public static TaskError unauthorized() {
        return new TaskError(Kind.UNAUTHORIZED, "unauthorized");
    }

    public static TaskError userNotProvisioned() {
        return new TaskError(Kind.USER_NOT_PROVISIONED, "user_not_provisioned");
    }

    public static TaskError invalidRequest() {
        return new TaskError(Kind.INVALID_REQUEST, "invalid_request");
    }

    public static TaskError invalidId() {
        return new TaskError(Kind.INVALID_ID, "invalid_id");
    }

    public static TaskError invalidStatus() {
        return new TaskError(Kind.INVALID_STATUS, "invalid_status");
    }

    public static TaskError invalidFinishedOn() {
        return new TaskError(Kind.INVALID_FINISHED_ON, "invalid_finished_on");
    }

    public static TaskError validation(String message) {
        return new TaskError(Kind.VALIDATION, message);
    }

    public static TaskError notFound() {
        return new TaskError(Kind.NOT_FOUND, "not_found");
    }

    public static TaskError internal(String message) {
        return new TaskError(Kind.INTERNAL, message);
    }

    /** 外部公開API(CONTRACT.mdセクション11): user_idクエリパラメータが無い/空 */
    public static TaskError userIdRequired() {
        return new TaskError(Kind.USER_ID_REQUIRED, "user_id_required");
    }

    /** 外部公開API: user_idクエリパラメータが数値でない */
    public static TaskError invalidUserId() {
        return new TaskError(Kind.INVALID_USER_ID, "invalid_user_id");
    }
}
