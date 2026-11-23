# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.HttpServerTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.TestSupport.HttpClient

  test "the listener has no connection cap" do
    assert :ranch.get_max_connections(ControlsGateway.HttpServer.listener()) == :infinity
  end

  test "health answers from the process alone and readiness waits for the pullers", %{
    http_port: port
  } do
    assert %{status: 200, body: "ok\n", headers: headers} =
             HttpClient.request(port, "GET", "/healthz")

    refute Map.has_key?(headers, "cache-control")

    assert %{status: 503, body: "not ready\n"} = HttpClient.request(port, "GET", "/readyz")
  end
end
