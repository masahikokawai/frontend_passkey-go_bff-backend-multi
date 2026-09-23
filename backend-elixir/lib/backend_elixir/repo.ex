defmodule BackendElixir.Repo do
  @moduledoc """
  Ecto.Repo(標準的作法として採用、README.md「アーキテクチャ選定」節参照)。
  接続先は`bff_gin_development`という、backend/migrations(golang-migrate)が正本として管理する
  既存のMySQLスキーマであり、このEcto.Repoはそこへの問い合わせ層としてのみ機能する。
  `mix ecto.migrate`等のマイグレーションコマンドは意図的に一切使わない(既存スキーマを
  Ectoのマイグレーション管理下に置かない、他言語と同じ「スキーマの正本はbackend/migrationsのみ」
  という方針を貫くため)
  """
  use Ecto.Repo,
    otp_app: :backend_elixir,
    adapter: Ecto.Adapters.MyXQL
end
