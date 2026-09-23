import Config

config :backend_elixir, ecto_repos: [BackendElixir.Repo]

# 接続先(host/port/user/password/database)はconfig時ではなく、Application.start/2内で
# BackendElixir.Config.from_env()から実行時に組み立てる(他言語のmain関数と同じく、
# 起動時に環境変数を読んで1箇所に集約する設計、config/runtime.exsという別ファイルには分けない)
config :backend_elixir, BackendElixir.Repo, migration_source: false

config :logger, :default_formatter, format: "$time $metadata[$level] $message\n"
