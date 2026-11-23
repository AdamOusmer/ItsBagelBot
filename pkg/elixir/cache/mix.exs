# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule PlatformCache.MixProject do
  use Mix.Project

  def project,
    do: [
      app: :platform_cache,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: [{:telemetry, "~> 1.0"}]
    ]

  def application, do: [extra_applications: [:logger]]
end
