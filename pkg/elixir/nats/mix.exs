# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule PlatformNats.MixProject do
  use Mix.Project

  def project,
    do: [
      app: :platform_nats,
      version: "0.1.0",
      elixir: "~> 1.17",
      elixirc_paths: elixirc_paths(Mix.env()),
      deps: deps()
    ]

  def application, do: [extra_applications: [:logger, :public_key, :ssl]]

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_env), do: ["lib"]

  defp deps,
    do: [
      {:gnat, "== 1.19.0"},
      {:platform_new_relic, path: "../new_relic"},
      {:telemetry, "~> 1.0"}
    ]
end
