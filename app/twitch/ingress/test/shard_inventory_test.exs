# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardInventoryTest do
  use ExUnit.Case, async: false
  @moduletag :capture_log

  alias Ingress.{AdminRpc, ConduitManager, ShardInventory, ShardSession}

  defmodule Children do
    use GenServer
    def start_link(reply), do: GenServer.start_link(__MODULE__, reply)
    def init(reply), do: {:ok, reply}
    def handle_call(:which_children, _, :silent), do: {:noreply, :silent}
    def handle_call(:which_children, _, children), do: {:reply, children, children}
  end

  defmodule Session do
    use GenServer
    def start_link(status), do: GenServer.start_link(__MODULE__, status)
    def init(status), do: {:ok, status}
    def handle_call(:status, _, status), do: {:reply, status, status}
  end

  test "a supervisor that misses the deadline yields an unknown inventory, not an empty one" do
    supervisor = start_supervised!({Children, :silent})

    assert {:error, :timeout} = ShardInventory.sessions(supervisor, 20)
    assert {:error, :timeout} = ShardInventory.unmanaged(supervisor, 20)
    assert {:error, :timeout} = ShardInventory.by_shard(supervisor, 20)
  end

  test "a dead supervisor yields an unknown inventory" do
    supervisor = start_supervised!({Children, []})
    stop_supervised!(Children)

    assert {:error, {:supervisor_unavailable, _}} = ShardInventory.unmanaged(supervisor, 20)
  end

  test "a supervisor with no children yields a known empty inventory" do
    supervisor = start_supervised!({Children, []})

    assert {:ok, []} = ShardInventory.unmanaged(supervisor, 100)
    assert {:ok, %{}} = ShardInventory.by_shard(supervisor, 100)
  end

  test "supervised sessions without a registry entry are reported as unmanaged" do
    start_supervised!({Horde.Registry, name: Ingress.Registry, keys: :unique, members: :auto})
    session = start_supervised!({Session, %{shard_id: 3, state: "connected"}})
    child = {:undefined, session, :worker, [ShardSession]}
    supervisor = start_supervised!({Children, [child]})

    assert {:ok, [{^session, %{shard_id: 3}}]} = ShardInventory.unmanaged(supervisor, 100)
    assert {:ok, %{3 => %{managed: false}}} = ShardInventory.by_shard(supervisor, 100)
  end

  test "the unmanaged sweep skips an unknown inventory without stopping anything" do
    assert :skipped = ConduitManager.sweep_unmanaged_shards({:error, :timeout}, 0)
    assert [] = ConduitManager.sweep_unmanaged_shards({:ok, []}, 0)
  end

  test "the fleet reply marks an unknown inventory unavailable instead of listing zero shards" do
    assert %{inventory: "unavailable", shards: shards} =
             AdminRpc.fleet({:error, :timeout}, [0, 4], 2)

    assert shards == [
             %{shard_id: 0, state: "unknown"},
             %{shard_id: 1, state: "unknown"},
             %{shard_id: 4, state: "unknown"}
           ]
  end

  test "the fleet reply keeps vacant states when the inventory is known" do
    live = %{shard_id: 1, state: "connected", managed: true}

    assert %{inventory: "available", shards: shards} =
             AdminRpc.fleet({:ok, %{1 => live}}, [0, 1], 3)

    assert shards == [
             %{shard_id: 0, state: "unresponsive", managed: true},
             live,
             %{shard_id: 2, state: "unregistered", managed: false}
           ]
  end
end
