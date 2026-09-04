# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.StatusPlug do
  @moduledoc """
  HTTP health surface, same contract as the Go services' pkg/health: /healthz
  is process liveness, /readyz gates on the critical checks, and /status
  reports every check as JSON for the Better Stack status page.

  The verdict itself lives in Ingress.Health, which Ingress.HealthRpc reads
  too, so the HTTP body and the NATS reply can never disagree about the same
  instant. /status answers 200 ok, 207 degraded, 503 down — see
  `Ingress.Health.http_status/1` for why the degraded case gets its own code
  instead of a keyword in the body.
  """

  @behaviour Plug
  import Plug.Conn

  alias Ingress.Health

  @impl true
  def init(opts), do: opts

  @impl true
  def call(%Plug.Conn{request_path: "/healthz"} = conn, _opts), do: send_resp(conn, 200, "ok\n")

  # Only "down" pulls the pod out of the Kubernetes endpoints: a degraded
  # ingress still takes traffic, and /status is where that shows up.
  def call(%Plug.Conn{request_path: "/readyz"} = conn, _opts) do
    if Health.up?(Health.report()) do
      send_resp(conn, 200, "ok\n")
    else
      send_resp(conn, 503, "not ready\n")
    end
  end

  def call(%Plug.Conn{request_path: "/status"} = conn, _opts) do
    report = Health.report()

    conn
    |> put_resp_content_type("application/json")
    |> put_resp_header("cache-control", "no-store")
    |> send_resp(Health.http_status(report.status), Jason.encode!(report))
  end

  def call(conn, _opts), do: send_resp(conn, 404, "not found\n")
end
