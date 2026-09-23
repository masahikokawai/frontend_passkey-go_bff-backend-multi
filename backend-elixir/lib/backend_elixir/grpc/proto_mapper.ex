defmodule BackendElixir.Grpc.ProtoMapper do
  @moduledoc "Domain.Task <-> Task.V1.Task(protobuf生成モジュール)の相互変換"

  alias BackendElixir.Domain.TaskInput
  alias BackendElixir.Domain.Validation

  def to_proto(task) do
    base = %Task.V1.Task{
      id: task.id,
      name: task.name,
      status: task.status_wire,
      finished_on: Date.to_iso8601(task.finished_on),
      labels: Enum.map(task.labels, fn label -> %Task.V1.Label{id: label.id, name: label.name} end),
      created_at: to_timestamp(task.created_at),
      updated_at: to_timestamp(task.updated_at)
    }

    if task.description do
      %{base | description: task.description}
    else
      base
    end
  end

  def from_create_request(%Task.V1.CreateTaskRequest{} = req) do
    with {:ok, finished_on} <- Validation.parse_finished_on(req.finished_on) do
      {:ok,
       %TaskInput{
         name: req.name,
         description: req.description,
         status_raw: req.status,
         finished_on: finished_on,
         label_ids: req.label_ids
       }}
    end
  end

  def from_update_request(%Task.V1.UpdateTaskRequest{} = req) do
    with {:ok, finished_on} <- Validation.parse_finished_on(req.finished_on) do
      {:ok,
       %TaskInput{
         name: req.name,
         description: req.description,
         status_raw: req.status,
         finished_on: finished_on,
         label_ids: req.label_ids
       }}
    end
  end

  # 【実機検証で確認する必要がある点、backend-python(FromDatetime、ナイーブなdatetimeをUTCとして
  # 扱う)/backend-java(JDBCのタイムゾーン変換バグ)と同じ観点】ここではNaiveDateTimeが
  # UTCの壁時計値そのものであることを前提に、明示的にUTCとしてDateTimeへ変換してから
  # Google.Protobuf.Timestampへ渡す(タイムゾーン変換は一切発生しない)
  defp to_timestamp(%NaiveDateTime{} = naive) do
    dt = DateTime.from_naive!(naive, "Etc/UTC")
    seconds = DateTime.to_unix(dt, :second)
    %Google.Protobuf.Timestamp{seconds: seconds, nanos: 0}
  end
end
