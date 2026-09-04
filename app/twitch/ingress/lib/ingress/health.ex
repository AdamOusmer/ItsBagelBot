# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Health do
  @moduledoc """
  The single place that decides whether this ingress is "ok", "degraded" or
  "down". Both surfaces read it — Ingress.StatusPlug for HTTP and
  Ingress.HealthRpc for the NATS reply — so the /status body and the RPC reply
  can never disagree about the same instant. Same report shape as the Go
  services' pkg/health: a service name, an aggregate status, and every check
  with its own verdict.

  The checks ride on gnat's registration behaviour: a connection's registered
  name exists only while the connection is established (Gnat.ConnectionSupervisor
  restarts it unregistered during reconnects), so `Process.whereis/1` is a
  truthful, zero-network connectivity probe — the same signal Ingress.Nats
  uses before publishing. Both planes are critical: RPC (:gnat) carries the
  control endpoints, BUS (:gnat_bus) carries the firehose.
  """

  @type check :: %{name: String.t(), ok: boolean(), latency_ms: non_neg_integer()}
  @type report :: %{service: String.t(), status: String.t(), checks: [check()]}

  @service "ingress"

  @doc """
  Runs every check once and folds it into the report both surfaces serve.
  """
  @spec report() :: report()
  def report do
    checks = checks()
    %{service: @service, status: aggregate(checks), checks: checks}
  end

  @doc """
  True while the service is not "down". Degraded counts as up: the pod still
  takes traffic, so /readyz keeps it in the Kubernetes endpoints and the RPC
  reply's legacy `ok` field keeps meaning what its readers assume.
  """
  @spec up?(report()) :: boolean()
  def up?(%{status: status}), do: status != "down"

  @doc """
  HTTP status code for a verdict: 200 ok, 207 degraded, 503 down. The code
  carries the same answer as the body so a monitor never has to read the body.

  207 (Multi-Status) is the honest code for a mixed answer and, being 2xx, it
  still reads green to any plain "expect 2xx" check. Better Stack then splits
  the two verdicts on expected-status-code lists alone: an availability monitor
  expecting "200,207" pages only on a real outage, while a second monitor
  expecting "200" notifies on the impairment without paging. The earlier design
  left degraded on 200 and put the word in the body, which needed a keyword
  monitor; status codes cost nothing on the free plan and cannot drift out of
  sync with the aggregate the way a body string can.
  """
  @spec http_status(String.t()) :: 200 | 207 | 503
  def http_status("down"), do: 503
  def http_status("degraded"), do: 207
  def http_status(_ok), do: 200

  @spec checks() :: [check()]
  def checks do
    [check("nats_rpc", :gnat), check("nats_bus", :gnat_bus)]
  end

  # latency_ms is a deliberate constant, not an unmeasured field: this probe is
  # one `Process.whereis/1` registry lookup with no network leg at all, so any
  # number measured here would be scheduler noise published as NATS latency.
  # The key stays in the payload because the Go report carries it and consumers
  # parse one shape.
  @spec check(String.t(), atom()) :: check()
  def check(name, connection) do
    %{name: name, ok: is_pid(Process.whereis(connection)), latency_ms: 0}
  end

  @doc """
  Folds per-check outcomes into the service verdict: any critical failure is
  down, otherwise any failure is degraded, otherwise ok.
  """
  @spec aggregate([check()]) :: String.t()
  def aggregate(checks) do
    cond do
      Enum.any?(checks, &(not &1.ok and not Map.get(&1, :optional, false))) -> "down"
      Enum.any?(checks, &(not &1.ok)) -> "degraded"
      true -> "ok"
    end
  end
end
