# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Http do
  @moduledoc "Plug response helpers that drop Plug's default cache-control."

  def put_headers(conn, headers) do
    Enum.reduce(headers, conn, fn {name, value}, conn ->
      Plug.Conn.put_resp_header(conn, name, value)
    end)
  end

  def reply(conn, status, headers \\ %{}, body \\ "") do
    conn
    |> Plug.Conn.delete_resp_header("cache-control")
    |> put_headers(headers)
    |> close_while_draining()
    |> Plug.Conn.send_resp(status, body)
  end

  defp close_while_draining(conn) do
    if ControlsGateway.Drain.draining?() and Plug.Conn.get_http_protocol(conn) == :"HTTP/1.1",
      do: Plug.Conn.put_resp_header(conn, "connection", "close"),
      else: conn
  end

  def json(conn, status, headers, body) do
    reply(conn, status, Map.put(headers, "content-type", "application/json"), Jason.encode!(body))
  end
end
