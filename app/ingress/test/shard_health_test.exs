defmodule Ingress.ShardHealthTest do
  use ExUnit.Case, async: true

  alias Ingress.ShardHealth

  @now ~U[2026-07-13 12:00:00Z]

  defp shard(id, status), do: %{"id" => to_string(id), "status" => status}

  describe "unhealthy_ids/2" do
    test "picks shards whose transport is not enabled" do
      shards = [
        shard(0, "websocket_disconnected"),
        shard(1, "enabled"),
        shard(2, "websocket_failed_ping_pong"),
        shard(3, "enabled"),
        shard(4, "enabled")
      ]

      assert ShardHealth.unhealthy_ids(shards, 5) == [0, 2]
    end

    test "ignores slots at or above the desired count (scale-down leftovers)" do
      shards = [shard(4, "websocket_disconnected"), shard(5, "websocket_disconnected")]

      assert ShardHealth.unhealthy_ids(shards, 5) == [4]
    end

    test "ignores malformed entries" do
      shards = [%{"id" => "not-a-number", "status" => "websocket_disconnected"}, %{}]

      assert ShardHealth.unhealthy_ids(shards, 5) == []
    end

    test "all healthy yields no work" do
      shards = for id <- 0..4, do: shard(id, "enabled")

      assert ShardHealth.unhealthy_ids(shards, 5) == []
    end
  end

  describe "heal_action/3" do
    test "unreachable session is restarted" do
      assert ShardHealth.heal_action(:unreachable, 1, @now) == :restart
    end

    test "session bound long ago but dead on Twitch is force-rebound" do
      probe = %{bound: true, bound_at: ~U[2026-07-13 11:00:00Z]}

      assert ShardHealth.heal_action(probe, 1, @now) == :force_rebind
    end

    test "session bound within the grace window is left settling" do
      probe = %{bound: true, bound_at: ~U[2026-07-13 11:59:30Z]}

      assert ShardHealth.heal_action(probe, 1, @now) == :skip
    end

    test "session with no bind timestamp counts as settled" do
      probe = %{bound: true, bound_at: nil}

      assert ShardHealth.heal_action(probe, 1, @now) == :force_rebind
    end

    test "unbound session is healing itself" do
      assert ShardHealth.heal_action(%{bound: false}, 1, @now) == :skip
    end

    test "persistent unhealth escalates to a restart whatever the session claims" do
      recently_bound = %{bound: true, bound_at: @now}

      assert ShardHealth.heal_action(%{bound: false}, 2, @now) == :restart
      assert ShardHealth.heal_action(recently_bound, 2, @now) == :restart
      assert ShardHealth.heal_action(%{bound: false}, 1, @now) == :skip
    end
  end

  describe "escalate?/1" do
    test "one unhealthy tick is the shadow of an in-flight reconnect" do
      refute ShardHealth.escalate?(1)
    end

    test "two consecutive unhealthy ticks exhaust self-healing" do
      assert ShardHealth.escalate?(2)
      assert ShardHealth.escalate?(3)
    end
  end
end
