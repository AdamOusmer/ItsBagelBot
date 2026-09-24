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
      {:libcluster, "~> 3.5"},
      {:horde, "~> 0.9"},
      {:gnat, "~> 1.10"},
      # Keep >= 2.19: EEF-CVE-2026-59248.
      {:cowlib, "~> 2.19"},
      {:mint_web_socket, "~> 1.0"},
      {:castore, "~> 1.0"},
      {:redix, "~> 1.9"},
      {:req, "~> 0.7"},
      {:jason, "~> 1.4"},
      {:bandit, "~> 1.8"},
      {:new_relic_agent, "~> 1.30"}
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
