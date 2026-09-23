defmodule BackendElixir.MixProject do
  use Mix.Project

  def project do
    [
      app: :backend_elixir,
      version: "0.1.0",
      elixir: "~> 1.20",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      elixirc_paths: elixirc_paths(Mix.env())
    ]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  def application do
    [
      extra_applications: [:logger, :crypto, :public_key, :inets],
      mod: {BackendElixir.Application, []}
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:myxql, "~> 0.7"},
      {:plug, "~> 1.16"},
      {:plug_cowboy, "~> 2.7"},
      {:jason, "~> 1.4"},
      {:grpc, "~> 0.9"},
      {:protobuf, "~> 0.13"},
      {:joken, "~> 2.6"},
      {:req, "~> 0.5"}
    ]
  end
end
