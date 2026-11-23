# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.BroadcasterCache do
  @moduledoc "Broadcaster lane cache on Platform.Cache: stale lane served while one reload runs."

  require Logger

  alias Platform.Cache

  @capacity 100_000
  @error_ttl_ms 5_000
  @lanes [:premium, :standard, :drop]

  def child_spec(opts) do
    %{id: {__MODULE__, name(opts)}, start: {__MODULE__, :start_link, [opts]}}
  end

  def start_link(opts \\ []) do
    name = name(opts)
    ttl_ms = Keyword.get(opts, :ttl_ms) || Ingress.Config.broadcaster_cache_ttl_ms()
    loader = Keyword.get(opts, :loader, &Ingress.BroadcasterStatus.lane_for/1)
    :persistent_term.put({__MODULE__, name}, loader)

    Cache.start_link(
      [
        name: name,
        ttl_ms: ttl_ms,
        stale_ms: ttl_ms,
        capacity: Keyword.get(opts, :capacity, @capacity),
        refresh_ahead: true,
        error_ttl_ms: @error_ttl_ms,
        error_default: :standard
      ] ++ invalidation(opts)
    )
  end

  @spec lane(String.t(), GenServer.server()) :: :premium | :standard | :drop
  def lane(broadcaster_id, server \\ __MODULE__) do
    loader = :persistent_term.get({__MODULE__, server})

    {:ok, lane} =
      Cache.get_or_load(server, broadcaster_id, fn -> load(loader, broadcaster_id) end)

    lane
  end

  @spec invalidate(String.t(), GenServer.server()) :: :ok
  def invalidate(broadcaster_id, server \\ __MODULE__),
    do: Cache.invalidate(server, broadcaster_id)

  @spec invalidate_all(GenServer.server()) :: :ok
  def invalidate_all(server \\ __MODULE__), do: Cache.clear(server)

  defp load(loader, id) do
    Ingress.Metrics.count("Cache/Loads")

    case loader.(id) do
      {:ok, lane} = loaded when lane in @lanes ->
        loaded

      {:error, reason} = failed ->
        Logger.warning("broadcaster status lookup failed for #{id}: #{inspect(reason)}")
        Ingress.Metrics.count("Cache/LoadErrors")
        failed
    end
  end

  defp invalidation(opts) do
    case Keyword.fetch(opts, :feed) do
      {:ok, feed} -> [invalidate_on: {feed, &dropped/1, [Keyword.fetch!(opts, :scope)]}]
      :error -> []
    end
  end

  defp dropped(%{id: :all}), do: :all
  defp dropped(%{id: id}) when is_integer(id), do: [Integer.to_string(id)]

  defp name(opts), do: Keyword.get(opts, :name, __MODULE__)
end
