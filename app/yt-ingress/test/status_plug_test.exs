# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.StatusPlugTest do
  use ExUnit.Case, async: true

  import Plug.Conn
  import Plug.Test

  @opts YtIngress.StatusPlug.init([])

  test "/healthz reports liveness" do
    conn = YtIngress.StatusPlug.call(conn(:get, "/healthz"), @opts)

    assert conn.status == 200
  end

  test "/status reports the service name and per-plane checks as JSON" do
    conn = YtIngress.StatusPlug.call(conn(:get, "/status"), @opts)

    assert conn.status in [200, 503]
    assert {"content-type", "application/json; charset=utf-8"} in conn.resp_headers

    body = Jason.decode!(conn.resp_body)
    assert body["service"] == "yt-ingress"

    check_names =
      body["checks"] |> Enum.map(& &1["name"]) |> Enum.sort()

    assert check_names == ["nats_bus", "nats_rpc"]
  end

  test "unknown paths 404" do
    conn = YtIngress.StatusPlug.call(conn(:get, "/"), @opts)

    assert conn.status == 404
  end
end
