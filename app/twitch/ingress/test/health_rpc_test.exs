# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HealthRpcTest do
  # Not async: the reply is now the live report, and StatusPlugTest registers
  # :gnat and :gnat_bus globally while it runs.
  use ExUnit.Case, async: false

  # No NATS connection runs under the test supervisor, so the report is down
  # with both planes named — the case that matters, since the pre-widening
  # reply would have answered ok:true here.
  test "replies with the whole report, not a liveness flag" do
    assert {:reply, body} = Ingress.HealthRpc.request(%{body: "{}"})

    assert %{"service" => "ingress", "ok" => false, "status" => "down", "checks" => checks} =
             Jason.decode!(body)

    assert Enum.map(checks, & &1["name"]) == ["nats_rpc", "nats_bus"]
    refute Enum.any?(checks, & &1["ok"])
  end
end
