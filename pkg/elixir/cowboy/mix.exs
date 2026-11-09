# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule PlatformCowboy.MixProject do
  use Mix.Project
  def project, do: [app: :platform_cowboy, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  def application, do: [extra_applications: [:logger, :ssl]]

  defp deps do
    [
      {:plug_cowboy, "~> 2.7"},
      {:cowboy,
       git: "https://github.com/ninenines/cowboy.git",
       ref: "3a630c07e1521abf63a1b9ea080509aa1337b60d",
       override: true},
      {:cowlib,
       git: "https://github.com/ninenines/cowlib.git",
       ref: "a3b15e29b3067644bfd580246c87f7af7a90b3b8",
       override: true},
      {:websock_adapter, "~> 0.5"},
      {:platform_websocket, path: "../websocket", only: :test}
    ]
  end
end
