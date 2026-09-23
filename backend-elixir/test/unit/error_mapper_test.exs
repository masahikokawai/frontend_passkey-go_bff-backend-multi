defmodule BackendElixir.Rest.ErrorMapperTest do
  use ExUnit.Case, async: true

  alias BackendElixir.Domain.TaskError
  alias BackendElixir.Rest.ErrorMapper

  test "unauthorized maps to 401" do
    assert {401, %{error: "unauthorized"}} = ErrorMapper.status_and_body(TaskError.unauthorized())
  end

  test "user_not_provisioned maps to 403" do
    assert {403, %{error: "user_not_provisioned"}} = ErrorMapper.status_and_body(TaskError.user_not_provisioned())
  end

  test "invalid_request maps to 400" do
    assert {400, %{error: "invalid_request"}} = ErrorMapper.status_and_body(TaskError.invalid_request())
  end

  test "invalid_id maps to 400" do
    assert {400, %{error: "invalid_id"}} = ErrorMapper.status_and_body(TaskError.invalid_id())
  end

  test "invalid_status maps to 422" do
    assert {422, %{error: "invalid_status"}} = ErrorMapper.status_and_body(TaskError.invalid_status())
  end

  test "invalid_finished_on maps to 422" do
    assert {422, %{error: "invalid_finished_on"}} = ErrorMapper.status_and_body(TaskError.invalid_finished_on())
  end

  test "validation maps to 422 with message" do
    assert {422, %{error: "validation_error", message: "nameは必須です"}} =
             ErrorMapper.status_and_body(TaskError.validation("nameは必須です"))
  end

  test "not_found maps to 404" do
    assert {404, %{error: "not_found"}} = ErrorMapper.status_and_body(TaskError.not_found())
  end

  test "internal maps to 500" do
    assert {500, %{error: "internal_server_error"}} = ErrorMapper.status_and_body(TaskError.internal("boom"))
  end
end
