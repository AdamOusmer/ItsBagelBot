# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule PlatformNewRelic.MixProject do
  use Mix.Project

  def project,
    do: [
      app: :platform_new_relic,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: [{:new_relic_agent, "== 1.42.0"}]
    ]

  def application, do: []
end
