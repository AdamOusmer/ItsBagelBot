# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Health do
  @type check :: %{name: String.t(), ok: boolean(), latency_ms: non_neg_integer()}
  @type report :: %{service: String.t(), status: String.t(), checks: [check()]}

  @service "ingress"

  @spec report() :: report()
  def report do
    checks = checks()
    %{service: @service, status: aggregate(checks), checks: checks}
  end

  @spec up?(report()) :: boolean()
  def up?(%{status: status}), do: status != "down"

  @spec http_status(String.t()) :: 200 | 207 | 503
  def http_status("down"), do: 503
  def http_status("degraded"), do: 207
  def http_status(_ok), do: 200

  @spec checks() :: [check()]
  def checks do
    [check("nats_rpc", :gnat), check("nats_bus", :gnat_bus)]
  end

  @spec check(String.t(), atom()) :: check()
  def check(name, connection) do
    %{name: name, ok: is_pid(Process.whereis(connection)), latency_ms: 0}
  end

  @spec aggregate([check()]) :: String.t()
  def aggregate(checks) do
    cond do
      Enum.any?(checks, &(not &1.ok and not Map.get(&1, :optional, false))) -> "down"
      Enum.any?(checks, &(not &1.ok)) -> "degraded"
      true -> "ok"
    end
  end
end
