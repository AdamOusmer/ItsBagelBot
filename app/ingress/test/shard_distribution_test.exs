defmodule Ingress.ShardDistributionTest do
  use ExUnit.Case, async: true

  alias Ingress.ShardDistribution

  defp member(node, status \\ :alive),
    do: %{name: {Ingress.ShardSupervisor, node}, status: status}

  defp shard_spec(id),
    do: %{
      id: {:shard, id},
      start: {Ingress.ShardSession, :start_link, [[shard_id: id, conduit_id: "c-1"]]},
      restart: :transient
    }

  test "round-robins shards across two nodes, 3/2 for five shards" do
    members = [member(:"ingress@node2"), member(:"ingress@node1")]

    placements =
      for id <- 0..4 do
        {:ok, %{name: {_sup, node}}} = ShardDistribution.choose_node(shard_spec(id), members)
        node
      end

    # Sorted by node name: node1 gets even ids, node2 odd ids.
    assert placements == [
             :"ingress@node1",
             :"ingress@node2",
             :"ingress@node1",
             :"ingress@node2",
             :"ingress@node1"
           ]
  end

  test "placement is deterministic regardless of member order" do
    a = [member(:"ingress@node1"), member(:"ingress@node2")]
    b = Enum.reverse(a)

    for id <- 0..4 do
      assert ShardDistribution.choose_node(shard_spec(id), a) ==
               ShardDistribution.choose_node(shard_spec(id), b)
    end
  end

  test "dead members receive nothing" do
    members = [member(:"ingress@node1"), member(:"ingress@node2", :dead)]

    for id <- 0..4 do
      assert {:ok, %{name: {_sup, :"ingress@node1"}}} =
               ShardDistribution.choose_node(shard_spec(id), members)
    end
  end

  test "no alive members is an error" do
    assert {:error, :no_alive_nodes} =
             ShardDistribution.choose_node(shard_spec(0), [member(:"ingress@node1", :dead)])
  end

  test "non-shard children fall back to uniform distribution" do
    members = [member(:"ingress@node1"), member(:"ingress@node2")]
    spec = %{id: :conduit_manager, start: {Ingress.ConduitManager, :start_link, [[]]}}

    assert {:ok, %{name: {_sup, node}}} = ShardDistribution.choose_node(spec, members)
    assert node in [:"ingress@node1", :"ingress@node2"]
  end
end
