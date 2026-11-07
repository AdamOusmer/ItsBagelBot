# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.MixProject do
  use Mix.Project

  def project do
    [
      app: :controls_gateway,
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
      mod: {ControlsGateway.Application, []}
    ]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_env), do: ["lib"]

  defp deps do
    [
      {:cowboy, "~> 2.13"},
      {:gnat, "== 1.19.0"},
      {:jason, "~> 1.4"},
      {:mint_web_socket, "~> 1.0", only: :test, runtime: false}
    ]
  end

  defp releases do
    [
      controls_gateway: [
        include_executables_for: [:unix],
        strip_beams: true
      ]
    ]
  end
end
