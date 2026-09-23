defmodule BackendElixir.Domain.Validation do
  @moduledoc """
  backend(Go)のservice.validateTaskInputと同じルール:
    - name: 必須・20コードポイント以内
    - finished_on: 過去日不可(UTC基準の「今日」)
    - status: enumの範囲内

  このプロジェクトは全言語で一貫して「ビジネスルールの検証を自前実装する」方針を貫いている
  (Ecto.Changesetのバリデーションヘルパー(validate_length等)には任せない)。Ecto.Changesetは
  構造的な型変換(パラメータ -> 型付き構造体、必須フィールドの有無)にのみ使い、ここでの
  ビジネスルール検証は他言語のTaskValidation/validate_task_input相当と同じ、明示的な関数として
  書く(README.md「アーキテクチャ選定」節参照)。

  【Elixir固有の落とし穴、他言語との対比】nameの文字数判定は「コードポイント数」で行う必要が
  あるが、Elixirの`String.length/1`はデフォルトで拡張書記素クラスタ(extended grapheme cluster、
  UAX #29)を数える。単純な絵文字1文字(コードポイント1個)ならグラフェム数=コードポイント数で
  一致するが、結合文字列やZWJシーケンス(例: 家族の絵文字のような複数コードポイントが1つの
  書記素にまとまるケース)では一致しない。Java/KotlinのString#length()がUTF-16コード単位数を
  返すために起きる「サロゲートペアの二重カウント」問題(過大カウント)とは逆に、Elixirの
  String.length/1は「グラフェム単位の過小カウント」の可能性がある、という別方向の落とし穴になる。
  ここでは`String.codepoints/1 |> length()`でコードポイント数を明示的に数える(Pythonのように`len()`が
  最初からコードポイントを返す、という「何もしなくて良い」ケースとも異なり、Elixirは意識して
  codepoints/1を選ぶ必要がある)
  """

  alias BackendElixir.Domain.{TaskError, TaskStatus}

  @max_name_codepoints 20

  def validate_task_input(input, today) do
    with :ok <- validate_name(input.name),
         :ok <- validate_finished_on(input.finished_on, today),
         {:ok, status_db, status_wire} <- validate_status(input.status_raw) do
      {:ok, status_db, status_wire}
    end
  end

  defp validate_name(nil), do: {:error, TaskError.validation("nameは必須です")}
  defp validate_name(""), do: {:error, TaskError.validation("nameは必須です")}

  defp validate_name(name) do
    codepoint_count = name |> String.codepoints() |> length()

    if codepoint_count > @max_name_codepoints do
      {:error, TaskError.validation("nameは20文字以内である必要があります")}
    else
      :ok
    end
  end

  defp validate_finished_on(nil, _today), do: {:error, TaskError.validation("finished_onに過去日は指定できません")}

  defp validate_finished_on(%Date{} = finished_on, %Date{} = today) do
    if Date.compare(finished_on, today) == :lt do
      {:error, TaskError.validation("finished_onに過去日は指定できません")}
    else
      :ok
    end
  end

  defp validate_status(raw) do
    case TaskStatus.from_wire_value(raw) do
      {:ok, db, wire} -> {:ok, db, wire}
      :error -> {:error, TaskError.validation(~s(不明なstatus: "#{raw}"))}
    end
  end

  @doc """
  カレンダー上の妥当性まで検証する(例: "2026-02-30"は文字列としては整形式だが実在しない日付)。
  `Date.from_iso8601/1`はこれを`{:error, :invalid_date}`で拒否するため、`invalid_finished_on`に
  マッピングする(backend-pythonのparse_finished_on/backend-kotlinのLocalDate.parseと同じ役割の分離:
  「形式として壊れている」はinvalid_finished_on、「形式は正しいが過去日」はvalidation_error)
  """
  def parse_finished_on(raw) when is_binary(raw) do
    case Date.from_iso8601(raw) do
      {:ok, date} -> {:ok, date}
      {:error, _reason} -> {:error, TaskError.invalid_finished_on()}
    end
  end

  def parse_finished_on(_), do: {:error, TaskError.invalid_finished_on()}
end
