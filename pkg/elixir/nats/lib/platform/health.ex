# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Health do
  @moduledoc "Service health report from NATS connection checks and boolean probes."

  @type check :: %{name: String.t(), ok: boolean(), latency_ms: non_neg_integer()}
  @type report :: %{service: String.t(), status: String.t(), checks: [check()]}

  @spec report(String.t(), [check() | {String.t(), (-> boolean())}]) :: report()
  def report(service, checks) do
    checks = Enum.map(checks, &run/1)
    %{service: service, status: aggregate(checks), checks: checks}
  end

  @spec up?(report()) :: boolean()
  def up?(%{status: status}), do: status != "down"

  @spec http_status(String.t()) :: 200 | 207 | 503
  def http_status("down"), do: 503
  def http_status("degraded"), do: 207
  def http_status(_ok), do: 200

  @spec check(String.t(), atom() | pid() | (-> boolean())) :: check()
  def check(name, probe) when is_function(probe, 0),
    do: %{name: name, ok: probe.() == true, latency_ms: 0}

  def check(name, connection),
    do: %{name: name, ok: Platform.Nats.connected?(connection), latency_ms: 0}

  @spec aggregate([check()]) :: String.t()
  def aggregate(checks) do
    cond do
      Enum.any?(checks, &(not &1.ok and not Map.get(&1, :optional, false))) -> "down"
      Enum.any?(checks, &(not &1.ok)) -> "degraded"
      true -> "ok"
    end
  end

  defp run({name, probe}), do: check(name, probe)
  defp run(%{} = check), do: check
end
