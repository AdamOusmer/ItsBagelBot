# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.MixProject do
  use Mix.Project

  def project do
    [
      app: :ingress,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      elixirc_paths: elixirc_paths(Mix.env()),
      deps: deps(),
      releases: releases()
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto, :ssl],
      mod: {Ingress.Application, []}
    ]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_env), do: ["lib"]

  defp deps do
    [
      {:platform_sift, path: "../../../pkg/elixir/sift"},
      {:platform_cowboy, path: "../../../pkg/elixir/cowboy"},
      {:platform_nats, path: "../../../pkg/elixir/nats"},
      {:platform_startup, path: "../../../pkg/elixir/startup"},
      {:platform_new_relic, path: "../../../pkg/elixir/new_relic"},
      {:platform_valkey, path: "../../../pkg/elixir/valkey"},
      {:platform_websocket, path: "../../../pkg/elixir/websocket"},
      {:libcluster, "~> 3.5"},
      {:horde, "~> 0.9"},
      {:castore, "~> 1.0"},
      {:req, "~> 0.7"},
      {:platform_boundary, path: "../../../pkg/elixir/boundary", only: :test}
    ]
  end

  defp releases do
    [
      ingress: [
        include_executables_for: [:unix],
        strip_beams: true
      ]
    ]
  end
end
