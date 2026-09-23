package com.bffgin.backend.rest;

import com.bffgin.backend.domain.TaskError;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

/**
 * backend-rustのerror.rsのテストスイートと1対1で対応する
 * (CONTRACT.mdセクション20.5のワイヤー契約パリティ、JSON形状・HTTPステータスを1文字も変えていない)
 */
class RestErrorMapperTest {

    @Test
    void unauthorizedMapsTo401() {
        var result = RestErrorMapper.statusAndBody(TaskError.unauthorized());
        assertEquals(401, result.status());
        assertEquals("unauthorized", result.body().get("error").asText());
    }

    @Test
    void userNotProvisionedMapsTo403() {
        var result = RestErrorMapper.statusAndBody(TaskError.userNotProvisioned());
        assertEquals(403, result.status());
        assertEquals("user_not_provisioned", result.body().get("error").asText());
    }

    @Test
    void invalidRequestMapsTo400() {
        var result = RestErrorMapper.statusAndBody(TaskError.invalidRequest());
        assertEquals(400, result.status());
        assertEquals("invalid_request", result.body().get("error").asText());
    }

    @Test
    void invalidIdMapsTo400() {
        var result = RestErrorMapper.statusAndBody(TaskError.invalidId());
        assertEquals(400, result.status());
        assertEquals("invalid_id", result.body().get("error").asText());
    }

    @Test
    void invalidStatusMapsTo422() {
        var result = RestErrorMapper.statusAndBody(TaskError.invalidStatus());
        assertEquals(422, result.status());
        assertEquals("invalid_status", result.body().get("error").asText());
    }

    @Test
    void invalidFinishedOnMapsTo422() {
        var result = RestErrorMapper.statusAndBody(TaskError.invalidFinishedOn());
        assertEquals(422, result.status());
        assertEquals("invalid_finished_on", result.body().get("error").asText());
    }

    @Test
    void validationMapsTo422WithMessage() {
        var result = RestErrorMapper.statusAndBody(TaskError.validation("nameは必須です"));
        assertEquals(422, result.status());
        assertEquals("validation_error", result.body().get("error").asText());
        assertEquals("nameは必須です", result.body().get("message").asText());
    }

    @Test
    void notFoundMapsTo404() {
        var result = RestErrorMapper.statusAndBody(TaskError.notFound());
        assertEquals(404, result.status());
        assertEquals("not_found", result.body().get("error").asText());
    }

    @Test
    void internalMapsTo500() {
        var result = RestErrorMapper.statusAndBody(TaskError.internal("boom"));
        assertEquals(500, result.status());
        assertEquals("internal_server_error", result.body().get("error").asText());
    }
}
