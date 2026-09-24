# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HealthRpcTest do
  use ExUnit.Case, async: false

  test "replies with the whole report, not a liveness flag" do
    assert {:reply, body} = Ingress.HealthRpc.request(%{body: "{}"})

    assert %{"service" => "ingress", "ok" => false, "status" => "down", "checks" => checks} =
             Jason.decode!(IO.iodata_to_binary(body))

    assert Enum.map(checks, & &1["name"]) == ["nats_rpc", "nats_bus"]
    refute Enum.any?(checks, & &1["ok"])
  end
end
