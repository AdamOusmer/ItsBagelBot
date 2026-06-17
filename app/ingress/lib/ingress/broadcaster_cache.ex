defmodule Ingress.BroadcasterCache do
  @moduledoc """
  In-process read-through cache for broadcaster lane status, so the hot chat
  path never hammers the owning service over NATS RPC.

  Reads are lock-free ETS lookups from the calling process. Misses funnel
  through this GenServer (serializing concurrent misses for the same key),
  which loads via `Ingress.BroadcasterStatus` and caches the answer with a
  TTL. Lookup failures are negative-cached briefly and answered with
  `:standard`, so an RPC outage degrades lanes rather than dropping messages
  or stampeding the data service.

  Entries are evicted by `Ingress.CacheInvalidator` when an invalidation key
  arrives on NATS (e.g. a broadcaster upgrades mid-stream).
  """

  use GenServer
  require Logger

  @default_table __MODULE__
  # How long a failed lookup is cached before retrying.
  @error_ttl_ms 5_000
  # Expired entries are dropped lazily on read; the sweep bounds the table for
  # broadcasters that are never read again.
  @sweep_interval_ms 60_000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @doc "Returns `:premium`, `:standard` or `:drop` for the broadcaster."
  @spec lane(String.t(), GenServer.server()) :: :premium | :standard | :drop
  def lane(broadcaster_id, server \\ __MODULE__) do
    table = table_name(server)

    case :ets.lookup(table, broadcaster_id) do
      [{^broadcaster_id, lane, expires_at}] ->
        if expires_at > now_ms() do
          lane
        else
          GenServer.call(server, {:lookup, broadcaster_id})
        end

      [] ->
        GenServer.call(server, {:lookup, broadcaster_id})
    end
  end

  @doc "Synchronous so that a read after invalidation never sees the stale entry."
  @spec invalidate(String.t(), GenServer.server()) :: :ok
  def invalidate(broadcaster_id, server \\ __MODULE__) do
    GenServer.call(server, {:invalidate, broadcaster_id})
  end

  @spec invalidate_all(GenServer.server()) :: :ok
  def invalidate_all(server \\ __MODULE__) do
    GenServer.call(server, :invalidate_all)
  end

  @impl true
  def init(opts) do
    table = Keyword.get(opts, :table, @default_table)
    :ets.new(table, [:set, :protected, :named_table, read_concurrency: true])
    sweep_interval_ms = Keyword.get(opts, :sweep_interval_ms, @sweep_interval_ms)
    Process.send_after(self(), :sweep, sweep_interval_ms)

    {:ok,
     %{
       table: table,
       ttl_ms: Keyword.get(opts, :ttl_ms) || Ingress.Config.broadcaster_cache_ttl_ms(),
       sweep_interval_ms: sweep_interval_ms,
       loader: Keyword.get(opts, :loader, &Ingress.BroadcasterStatus.lane_for/1)
     }}
  end

  @impl true
  def handle_call({:lookup, id}, _from, state) do
    # Double-check: another caller may have filled the entry while this one
    # was queued behind it.
    case :ets.lookup(state.table, id) do
      [{^id, lane, expires_at}] when expires_at > 0 ->
        if expires_at > now_ms() do
          {:reply, lane, state}
        else
          {:reply, load(id, state), state}
        end

      _ ->
        {:reply, load(id, state), state}
    end
  end

  def handle_call({:invalidate, id}, _from, state) do
    :ets.delete(state.table, id)
    {:reply, :ok, state}
  end

  def handle_call(:invalidate_all, _from, state) do
    :ets.delete_all_objects(state.table)
    {:reply, :ok, state}
  end

  @impl true
  def handle_info(:sweep, state) do
    now = now_ms()
    :ets.select_delete(state.table, [{{:_, :_, :"$1"}, [{:"=<", :"$1", now}], [true]}])
    Process.send_after(self(), :sweep, state.sweep_interval_ms)
    {:noreply, state}
  end

  defp load(id, state) do
    case state.loader.(id) do
      {:ok, lane} when lane in [:premium, :standard, :drop] ->
        Ingress.Metrics.count("Cache/Loads")
        :ets.insert(state.table, {id, lane, now_ms() + state.ttl_ms})
        lane

      {:error, reason} ->
        Logger.warning("broadcaster status lookup failed for #{id}: #{inspect(reason)}")
        Ingress.Metrics.count("Cache/LoadErrors")
        :ets.insert(state.table, {id, :standard, now_ms() + @error_ttl_ms})
        :standard
    end
  end

  defp table_name(__MODULE__), do: @default_table
  defp table_name(server) when is_atom(server), do: server
  defp table_name(_), do: @default_table

  defp now_ms, do: System.monotonic_time(:millisecond)
end
