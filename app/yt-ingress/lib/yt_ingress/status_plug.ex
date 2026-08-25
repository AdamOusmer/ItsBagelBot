# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.StatusPlug do
  @moduledoc """
  HTTP health surface, same contract as the Go services' pkg/health: /healthz
  is process liveness, /readyz gates on the critical checks, and /status
  reports every check as JSON for the Better Stack status page ("ok" |
  "degraded" | "down", HTTP 503 only when down).

  The checks ride on gnat's registration behaviour: a connection's registered
  name exists only while the connection is established (Gnat.ConnectionSupervisor
  restarts it unregistered during reconnects), so `Process.whereis/1` is a
  truthful, zero-network connectivity probe — the same signal YtIngress.Nats
  uses before publishing. Both planes are critical: RPC (:gnat) carries the
  control endpoints, BUS (:gnat_bus) carries the firehose.
  """

  @behaviour Plug
  import Plug.Conn

  @impl true
  def init(opts), do: opts

  @impl true
  def call(%Plug.Conn{request_path: "/healthz"} = conn, _opts), do: send_resp(conn, 200, "ok\n")

  def call(%Plug.Conn{request_path: "/readyz"} = conn, _opts) do
    case aggregate(checks()) do
      "down" -> send_resp(conn, 503, "not ready\n")
      _up -> send_resp(conn, 200, "ok\n")
    end
  end

  def call(%Plug.Conn{request_path: "/status"} = conn, _opts) do
    checks = checks()
    status = aggregate(checks)
    body = Jason.encode!(%{service: "yt-ingress", status: status, checks: checks})

    conn
    |> put_resp_content_type("application/json")
    |> put_resp_header("cache-control", "no-store")
    |> send_resp(if(status == "down", do: 503, else: 200), body)
  end

  def call(conn, _opts), do: send_resp(conn, 404, "not found\n")

  defp checks do
    [check("nats_rpc", :gnat), check("nats_bus", :gnat_bus)]
  end

  defp check(name, connection) do
    %{name: name, ok: is_pid(Process.whereis(connection)), latency_ms: 0}
  end

  defp aggregate(checks) do
    cond do
      Enum.any?(checks, &(not &1.ok and not Map.get(&1, :optional, false))) -> "down"
      Enum.any?(checks, &(not &1.ok)) -> "degraded"
      true -> "ok"
    end
  end
end
