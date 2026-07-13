defmodule Ingress.CapacityTest do
  use ExUnit.Case, async: true

  alias Ingress.Capacity

  test "reports the rounded production-shaped benchmark and 75% target" do
    assert Capacity.pod_rated_eps() == 140_000
    assert Capacity.pod_target_eps() == 105_000
    assert Capacity.nats_rated_eps() == 86_000
    assert Capacity.nats_target_eps() == 64_500
    assert Capacity.websocket_rated_eps() == 12_500
    assert Capacity.websocket_target_eps() == 9_375
    assert Capacity.target_utilization_pct() == 75
  end

  test "fleet capacity grows with live ingress nodes, not shard count" do
    assert %{
             fleet_nodes: 5,
             fleet_rated_eps: 700_000,
             fleet_target_eps: 525_000,
             nats_rated_eps: 86_000,
             effective_rated_eps: 86_000,
             effective_target_eps: 64_500,
             bottleneck: "nats",
             pod_rated_eps: 140_000,
             websocket_rated_eps: 12_500
           } = Capacity.snapshot(5)
  end
end
