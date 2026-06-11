defmodule Ingress.MixProject do
  use Mix.Project

  def project do
    [
      app: :ingress,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
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

  defp deps do
    [
      # BEAM node auto-discovery
      {:libcluster, "~> 3.5"},
      # Cluster-wide registry + dynamic supervisor (shard ownership)
      {:horde, "~> 0.9"},
      # NATS client
      {:gnat, "~> 1.10"},
      # Raw WebSocket over Mint: the process owns the socket lifecycle
      {:mint_web_socket, "~> 1.0"},
      {:castore, "~> 1.0"},
      # Twitch Helix HTTP API
      {:req, "~> 0.5"},
      {:jason, "~> 1.4"},
      # New Relic monitoring (disabled automatically when no license key)
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
