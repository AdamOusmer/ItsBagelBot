# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.Failback do
  @moduledoc "Settings shared by the leaf and hub failback loops, read from the same variables as pkg/bus/failback.go."

  def enabled?, do: node_name() != ""
  def node_name, do: System.get_env("NODE_NAME", "")
  def interval, do: positive_env("NATS_FAILBACK_INTERVAL_MS", 30_000)
  def required, do: positive_env("NATS_FAILBACK_SUCCESSES", 3)
  def probe_timeout, do: positive_env("NATS_FAILBACK_PROBE_TIMEOUT_MS", 1_000)

  defp positive_env(key, fallback) do
    case Integer.parse(System.get_env(key, "")) do
      {value, ""} when value > 0 -> value
      _ -> fallback
    end
  end
end
