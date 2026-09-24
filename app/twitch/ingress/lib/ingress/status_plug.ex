# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.StatusPlug do
  @behaviour Plug
  import Plug.Conn

  alias Ingress.{Health, JSON}

  @impl true
  def init(opts), do: opts

  @impl true
  def call(%Plug.Conn{request_path: "/healthz"} = conn, _opts), do: send_resp(conn, 200, "ok\n")

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
    |> send_resp(Health.http_status(report.status), JSON.encode(report))
  end

  def call(conn, _opts), do: send_resp(conn, 404, "not found\n")
end
