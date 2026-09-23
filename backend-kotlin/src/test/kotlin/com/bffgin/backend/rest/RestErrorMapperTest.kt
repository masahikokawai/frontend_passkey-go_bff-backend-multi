package com.bffgin.backend.rest

import com.bffgin.backend.domain.TaskError
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

/**
 * backend-java/backend-rustのerror.rsのテストスイートと1対1で対応する
 * (CONTRACT.mdセクション20.5のワイヤー契約パリティ、JSON形状・HTTPステータスを1文字も変えていない)
 */
class RestErrorMapperTest {

    @Test
    fun unauthorizedMapsTo401() {
        val result = RestErrorMapper.statusAndBody(TaskError.unauthorized())
        assertEquals(401, result.status)
        assertEquals("unauthorized", result.body.get("error").asText())
    }

    @Test
    fun userNotProvisionedMapsTo403() {
        val result = RestErrorMapper.statusAndBody(TaskError.userNotProvisioned())
        assertEquals(403, result.status)
        assertEquals("user_not_provisioned", result.body.get("error").asText())
    }

    @Test
    fun invalidRequestMapsTo400() {
        val result = RestErrorMapper.statusAndBody(TaskError.invalidRequest())
        assertEquals(400, result.status)
        assertEquals("invalid_request", result.body.get("error").asText())
    }

    @Test
    fun invalidIdMapsTo400() {
        val result = RestErrorMapper.statusAndBody(TaskError.invalidId())
        assertEquals(400, result.status)
        assertEquals("invalid_id", result.body.get("error").asText())
    }

    @Test
    fun invalidStatusMapsTo422() {
        val result = RestErrorMapper.statusAndBody(TaskError.invalidStatus())
        assertEquals(422, result.status)
        assertEquals("invalid_status", result.body.get("error").asText())
    }

    @Test
    fun invalidFinishedOnMapsTo422() {
        val result = RestErrorMapper.statusAndBody(TaskError.invalidFinishedOn())
        assertEquals(422, result.status)
        assertEquals("invalid_finished_on", result.body.get("error").asText())
    }

    @Test
    fun validationMapsTo422WithMessage() {
        val result = RestErrorMapper.statusAndBody(TaskError.validation("nameは必須です"))
        assertEquals(422, result.status)
        assertEquals("validation_error", result.body.get("error").asText())
        assertEquals("nameは必須です", result.body.get("message").asText())
    }

    @Test
    fun notFoundMapsTo404() {
        val result = RestErrorMapper.statusAndBody(TaskError.notFound())
        assertEquals(404, result.status)
        assertEquals("not_found", result.body.get("error").asText())
    }

    @Test
    fun internalMapsTo500() {
        val result = RestErrorMapper.statusAndBody(TaskError.internal("boom"))
        assertEquals(500, result.status)
        assertEquals("internal_server_error", result.body.get("error").asText())
    }
}
