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
      # `Ingress.Nats.Publisher.Wire` encodes NATS message headers with
      # `:cow_http.headers/1`. gnat pulled cowlib in until 1.16.0 dropped it, so
      # the call survived on an undeclared transitive dep; declare it directly.
      # The floor is also a security floor: cowlib below 2.19.0 carries
      # EEF-CVE-2026-59248 (unbounded HPACK/QPACK decoding, memory-exhaustion DoS).
      {:cowlib, "~> 2.19"},
      # Raw WebSocket over Mint: the process owns the socket lifecycle
      {:mint_web_socket, "~> 1.0"},
      {:castore, "~> 1.0"},
      # Twitch Helix HTTP API
      {:req, "~> 0.7"},
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
