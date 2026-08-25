# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.MixProject do
  use Mix.Project

  def project do
    [
      app: :yt_ingress,
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
      mod: {YtIngress.Application, []}
    ]
  end

  defp deps do
    [
      # BEAM node auto-discovery
      {:libcluster, "~> 3.5"},
      # Cluster-wide registry + dynamic supervisor (chat session ownership)
      {:horde, "~> 0.9"},
      # NATS client
      {:gnat, "~> 1.10"},
      # gRPC client for the YouTube liveChatMessages.streamList server stream.
      # The stubs under lib/yt_ingress/youtube/pb are generated from the
      # vendored priv/protos/stream_list.proto against this package's runtime;
      # see that proto header for how to regenerate.
      {:grpc, "~> 1.0"},
      # Protobuf runtime the generated modules encode against
      {:protobuf, "~> 0.17"},
      # `YtIngress.Nats` keeps the same cowlib floor as the Twitch ingress:
      # below 2.19.0 cowlib carries EEF-CVE-2026-59248 (unbounded HPACK/QPACK
      # decoding, memory-exhaustion DoS).
      {:cowlib, "~> 2.19"},
      # YouTube Data API (discovery of active broadcasts per channel)
      {:req, "~> 0.7"},
      {:jason, "~> 1.4"},
      # HTTP health surface (YtIngress.StatusPlug): k8s probes + the Better
      # Stack status page, TLS-terminated in-process like the Go services.
      {:bandit, "~> 1.8"},
      # New Relic monitoring (disabled automatically when no license key)
      {:new_relic_agent, "~> 1.30"}
    ]
  end

  defp releases do
    [
      yt_ingress: [
        include_executables_for: [:unix],
        strip_beams: true
      ]
    ]
  end
end
