defmodule BackendElixir.Grpc.Endpoint do
  @moduledoc "内部gRPC v2(:9104)。TaskServiceのみを提供するエンドポイント"
  use GRPC.Endpoint

  run BackendElixir.Grpc.TaskService
end
