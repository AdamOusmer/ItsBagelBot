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
       ref: "127b12e4760d5d5321ee9ebe5a52e59f97af3403",
       override: true},
      {:cowlib,
       git: "https://github.com/ninenines/cowlib.git",
       ref: "585ef0860e266b73628dd5c9efeb734106233f36",
       override: true},
      {:ranch,
       git: "https://github.com/ninenines/ranch.git",
       ref: "b39d299bcaf41a76369acae04adcee2d5c332ecf",
       override: true},
      {:websock_adapter, "~> 0.5"},
      {:platform_websocket, path: "../websocket", only: :test}
    ]
  end
end
