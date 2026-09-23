from __future__ import annotations

from app.domain.errors import TaskError
from app.rest.error_mapper import status_and_body


def test_unauthorized_maps_to_401() -> None:
    result = status_and_body(TaskError.unauthorized())
    assert result.status == 401
    assert result.body["error"] == "unauthorized"


def test_user_not_provisioned_maps_to_403() -> None:
    result = status_and_body(TaskError.user_not_provisioned())
    assert result.status == 403
    assert result.body["error"] == "user_not_provisioned"


def test_invalid_request_maps_to_400() -> None:
    result = status_and_body(TaskError.invalid_request())
    assert result.status == 400
    assert result.body["error"] == "invalid_request"


def test_invalid_id_maps_to_400() -> None:
    result = status_and_body(TaskError.invalid_id())
    assert result.status == 400
    assert result.body["error"] == "invalid_id"


def test_invalid_status_maps_to_422() -> None:
    result = status_and_body(TaskError.invalid_status())
    assert result.status == 422
    assert result.body["error"] == "invalid_status"


def test_invalid_finished_on_maps_to_422() -> None:
    result = status_and_body(TaskError.invalid_finished_on())
    assert result.status == 422
    assert result.body["error"] == "invalid_finished_on"


def test_validation_maps_to_422_with_message() -> None:
    result = status_and_body(TaskError.validation("nameは必須です"))
    assert result.status == 422
    assert result.body["error"] == "validation_error"
    assert result.body["message"] == "nameは必須です"


def test_not_found_maps_to_404() -> None:
    result = status_and_body(TaskError.not_found())
    assert result.status == 404
    assert result.body["error"] == "not_found"


def test_internal_maps_to_500() -> None:
    result = status_and_body(TaskError.internal("boom"))
    assert result.status == 500
    assert result.body["error"] == "internal_server_error"
