# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardScalerTest do
  use ExUnit.Case, async: false

  alias Ingress.ShardScaler

  defmodule FakeShard do
    use GenServer

    def start_link(shard_id) do
      GenServer.start_link(__MODULE__, shard_id,
        name: {:via, Horde.Registry, {Ingress.Registry, {:shard, shard_id}}}
      )
    end

    def child_spec(shard_id),
      do: %{id: {__MODULE__, shard_id}, start: {__MODULE__, :start_link, [shard_id]}}

    @impl true
    def init(shard_id), do: {:ok, shard_id}

    @impl true
    def handle_call(:status, _from, shard_id),
      do: {:reply, %{shard_id: shard_id, load: 8}, shard_id}
  end

  @configured_shards 5

  setup do
    Ingress.EnvCase.put_env(conduit_shard_count: @configured_shards, max_shards: 11)
    start_supervised!({Horde.Registry, name: Ingress.Registry, keys: :unique, members: :auto})
    for id <- 0..(@configured_shards - 1), do: start_supervised!({FakeShard, id})
    :ok
  end

  defp autoscale_ticks(count) do
    scaler = GenServer.whereis(ShardScaler.via())
    for _ <- 1..count, do: send(scaler, :autoscale_tick)
    :sys.get_state(scaler)
  end

  test "idle load keeps the configured shard count across scaler restarts" do
    start_supervised!(ShardScaler)
    assert ShardScaler.desired() == @configured_shards

    autoscale_ticks(12)
    assert ShardScaler.desired() == @configured_shards

    stop_supervised!(ShardScaler)
    start_supervised!(ShardScaler)
    assert ShardScaler.desired() == @configured_shards
  end

  test "a manual target below the configured shard count is held at it" do
    start_supervised!(ShardScaler)

    assert ShardScaler.set_target(1) == :ok
    assert ShardScaler.desired() == @configured_shards
  end

  test "a configured shard count above the ceiling is capped at it" do
    Ingress.EnvCase.put_env(conduit_shard_count: 13)
    start_supervised!(ShardScaler)

    assert ShardScaler.desired() == 11
    autoscale_ticks(3)
    assert ShardScaler.desired() == 11
  end
end
