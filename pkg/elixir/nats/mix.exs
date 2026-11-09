# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule PlatformNats.MixProject do
  use Mix.Project
  def project, do: [app: :platform_nats, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  def application, do: [extra_applications: [:logger, :ssl]]

  defp deps,
    do: [
      {:gnat, "== 1.19.0"},
      {:telemetry, "~> 1.0"}
    ]
end
