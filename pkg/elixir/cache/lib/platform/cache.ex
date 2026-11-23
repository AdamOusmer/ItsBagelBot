# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Cache do
  @moduledoc "ETS cache with single-flight loads, invalidation generations and a stale-on-error window."
  use GenServer

  @stripes 1024
  @epoch 1
  @default_load_timeout_ms 15_000
  @fresh_until 2
  @stale_until 3
  @row_generation 5
  @row_refreshing 6

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  def child_spec(opts) do
    %{id: {__MODULE__, Keyword.fetch!(opts, :name)}, start: {__MODULE__, :start_link, [opts]}}
  end

  @spec get_or_load(atom(), term(), (-> {:ok, term()} | {:skip, term()} | {:error, term()})) ::
          {:ok, term()} | {:error, term()}
  def get_or_load(cache, key, loader) when is_function(loader, 0) do
    config = config(cache)

    case lookup(config, key, @fresh_until) do
      {:ok, _value} = hit ->
        emit(cache, :hit)
        hit

      :miss ->
        miss(cache, config, key, loader)
    end
  end

  defp miss(cache, config, key, loader) do
    case serve_stale(config, key) do
      {:ok, _value} = stale ->
        emit(cache, :stale)
        if claim_refresh(config, key), do: GenServer.cast(cache, {:refresh, key, loader})
        stale

      :miss ->
        emit(cache, :miss)
        GenServer.call(cache, {:load, key, loader}, :infinity)
    end
  end

  defp claim_refresh(config, key) do
    case :ets.lookup(config.table, key) do
      [row] when elem(row, @row_refreshing) == false ->
        claimed = put_elem(row, @row_refreshing, true)
        :ets.select_replace(config.table, [{row, [], [{:const, claimed}]}]) == 1

      _ ->
        false
    end
  end

  defp serve_stale(%{refresh_ahead: true} = config, key) do
    with {:ok, value} = stale <- lookup(config, key, @stale_until),
         true <- config.stale_if.(value) do
      stale
    else
      _ -> :miss
    end
  end

  defp serve_stale(_config, _key), do: :miss

  @spec invalidate(atom(), term()) :: :ok
  def invalidate(cache, key), do: invalidate_key(config(cache), key)

  @spec invalidate_where(atom(), (term() -> boolean())) :: :ok
  def invalidate_where(cache, fun) when is_function(fun, 1),
    do: GenServer.call(cache, {:invalidate_where, fun})

  @spec clear(atom()) :: :ok
  def clear(cache) do
    config = config(cache)
    :atomics.add(config.generations, @epoch, 1)
    :ets.delete_all_objects(config.table)
    :ok
  end

  @spec size(atom()) :: non_neg_integer()
  def size(cache), do: :ets.info(config(cache).table, :size)

  def invalidate_on(cache, feed, keys_fun, scopes) when is_function(keys_fun, 1) do
    {feed_module, server} = feed_ref(feed)

    for scope <- scopes do
      feed_module.watch(
        server,
        scope,
        fn event -> when_alive(cache, fn -> drop(cache, keys_fun.(event)) end) end,
        cache
      )
    end

    feed_module.watch_gap(
      server,
      fn ->
        when_alive(cache, fn ->
          emit(cache, :gap)
          clear(cache)
        end)
      end,
      cache
    )

    :ok
  end

  defp when_alive(cache, fun) do
    if :ets.whereis(cache) != :undefined, do: fun.()
    :ok
  end

  defp drop(cache, :all), do: clear(cache)
  defp drop(cache, {:where, fun}), do: GenServer.cast(cache, {:invalidate_where, fun})
  defp drop(cache, keys) when is_list(keys), do: Enum.each(keys, &invalidate(cache, &1))

  defp feed_ref({module, server}), do: {module, server}
  defp feed_ref(server), do: {Platform.Invalidations, server}

  @impl true
  def init(opts) do
    config = %{
      table: Keyword.fetch!(opts, :name),
      generations: :atomics.new(@stripes + 1, signed: false),
      ttl_ms: Keyword.fetch!(opts, :ttl_ms),
      capacity: Keyword.fetch!(opts, :capacity),
      stale_ms: Keyword.get(opts, :stale_ms, 0),
      stale_if: Keyword.get(opts, :stale_if, fn _value -> true end),
      refresh_ahead: Keyword.get(opts, :refresh_ahead, false),
      error_ttl_ms: Keyword.get(opts, :error_ttl_ms),
      error_default: Keyword.fetch(opts, :error_default),
      load_timeout_ms: Keyword.get(opts, :load_timeout_ms, @default_load_timeout_ms)
    }

    :ets.new(config.table, [:set, :public, :named_table, read_concurrency: true])
    :persistent_term.put({__MODULE__, config.table}, config)
    register(config.table, Keyword.get(opts, :invalidate_on))
    {:ok, %{config: config, flights: %{}, by_key: %{}}}
  end

  defp register(_cache, nil), do: :ok

  defp register(cache, {feed, keys_fun, scopes}),
    do: invalidate_on(cache, feed, keys_fun, scopes)

  @impl true
  def handle_call({:load, key, loader}, from, state) do
    case lookup(state.config, key, @fresh_until) do
      {:ok, _value} = hit -> {:reply, hit, state}
      :miss -> {:noreply, join_or_start(state, key, loader, from)}
    end
  end

  def handle_call({:invalidate_where, fun}, _from, state) do
    invalidate_matching(state, fun)
    {:reply, :ok, state}
  end

  @impl true
  def handle_cast({:invalidate_where, fun}, state) do
    invalidate_matching(state, fun)
    {:noreply, state}
  end

  def handle_cast({:refresh, key, loader}, state) do
    case lookup(state.config, key, @fresh_until) do
      {:ok, _value} -> {:noreply, state}
      :miss -> {:noreply, join_or_start(state, key, loader, nil)}
    end
  end

  defp invalidate_matching(state, fun) do
    config = state.config

    stored =
      config.table
      |> :ets.select([{{:"$1", :_, :_, :_, :_, :_, :_}, [], [:"$1"]}])
      |> Enum.filter(fun)

    loading = for {_pid, %{key: key}} <- state.flights, fun.(key), do: key
    Enum.each(stored ++ loading, &invalidate_key(config, &1))
  end

  @impl true
  def handle_info({:loaded, pid, result}, state), do: {:noreply, finish(state, pid, result)}

  def handle_info({:DOWN, _ref, :process, pid, reason}, state),
    do: {:noreply, finish(state, pid, {:error, {:loader_exit, reason}})}

  def handle_info({:load_timeout, pid}, state) do
    if Map.has_key?(state.flights, pid), do: Process.exit(pid, :kill)
    {:noreply, state}
  end

  defp join_or_start(state, key, loader, from) do
    generation = generation(state.config, key)

    case Map.fetch(state.by_key, {key, generation}) do
      {:ok, pid} ->
        update_in(state.flights[pid].waiters, &add_waiter(&1, from))

      :error ->
        start_flight(
          state,
          %{key: key, generation: generation, waiters: add_waiter([], from)},
          loader
        )
    end
  end

  defp add_waiter(waiters, nil), do: waiters
  defp add_waiter(waiters, from), do: [from | waiters]

  defp start_flight(state, seed, loader) do
    server = self()
    {pid, ref} = spawn_monitor(fn -> send(server, {:loaded, self(), loader.()}) end)
    timer = Process.send_after(server, {:load_timeout, pid}, state.config.load_timeout_ms)
    flight = Map.merge(seed, %{ref: ref, timer: timer})

    %{
      state
      | flights: Map.put(state.flights, pid, flight),
        by_key: Map.put(state.by_key, {seed.key, seed.generation}, pid)
    }
  end

  defp finish(state, pid, result) do
    case Map.pop(state.flights, pid) do
      {nil, _flights} ->
        state

      {flight, flights} ->
        Process.demonitor(flight.ref, [:flush])
        Process.cancel_timer(flight.timer)
        reply = settle(state.config, flight, result)
        :ets.update_element(state.config.table, flight.key, {@row_refreshing + 1, false})
        Enum.each(flight.waiters, &GenServer.reply(&1, reply))
        by_key = Map.delete(state.by_key, {flight.key, flight.generation})
        %{state | flights: flights, by_key: by_key}
    end
  end

  defp settle(config, flight, {:ok, value}) do
    store(config, flight, value, jittered(config.ttl_ms))
    {:ok, value}
  end

  defp settle(_config, _flight, {:skip, value}), do: {:ok, value}

  defp settle(config, flight, {:error, reason}) do
    with {:ok, value} = stale <- lookup(config, flight.key, @stale_until),
         true <- config.stale_if.(value) do
      emit(config.table, :stale)
      retain(config, flight, value)
      stale
    else
      _ -> fail(config, flight, reason)
    end
  end

  defp retain(%{error_ttl_ms: nil}, _flight, _value), do: :ok
  defp retain(config, flight, value), do: store(config, flight, value, config.error_ttl_ms)

  defp fail(%{error_default: {:ok, value}, error_ttl_ms: ttl_ms} = config, flight, _reason)
       when is_integer(ttl_ms) do
    store(config, flight, value, ttl_ms)
    {:ok, value}
  end

  defp fail(_config, _flight, reason), do: {:error, reason}

  defp store(config, flight, value, ttl_ms) do
    if generation(config, flight.key) == flight.generation,
      do: insert(config, flight, value, ttl_ms)
  end

  defp insert(config, flight, value, ttl_ms) do
    now = now_ms()
    make_room(config, flight.key, now)
    fresh_until = now + ttl_ms

    :ets.insert(
      config.table,
      {flight.key, value, fresh_until, fresh_until + config.stale_ms,
       System.unique_integer([:monotonic]), flight.generation, false}
    )
  end

  defp jittered(ttl_ms), do: ttl_ms + :rand.uniform(div(ttl_ms, 10) + 1) - 1

  defp make_room(config, key, now) do
    if full?(config) and not :ets.member(config.table, key) do
      :ets.select_delete(config.table, [
        {{:_, :_, :_, :"$1", :_, :_, :_}, [{:"=<", :"$1", now}], [true]}
      ])

      evict_oldest(config)
    end
  end

  defp full?(config), do: :ets.info(config.table, :size) >= config.capacity

  defp evict_oldest(config) do
    if full?(config) do
      batch = max(1, div(config.capacity, 10))

      config.table
      |> :ets.select([{{:"$1", :_, :_, :_, :"$2", :_, :_}, [], [{{:"$2", :"$1"}}]}])
      |> Enum.sort()
      |> Enum.take(batch)
      |> Enum.each(fn {_order, key} -> :ets.delete(config.table, key) end)
    end
  end

  defp lookup(config, key, bound) do
    with [row] <- :ets.lookup(config.table, key),
         true <- elem(row, bound) > now_ms(),
         true <- elem(row, @row_generation) == generation(config, key) do
      {:ok, elem(row, 1)}
    else
      _ -> :miss
    end
  end

  defp invalidate_key(config, key) do
    bump(config, key)

    case :ets.take(config.table, key) do
      [] -> :ok
      [_row] -> emit(config.table, :invalidated)
    end
  end

  defp emit(name, event),
    do: :telemetry.execute([:platform, :cache, event], %{count: 1}, %{name: name})

  defp bump(config, key), do: :atomics.add(config.generations, stripe(key), 1)

  defp generation(config, key),
    do: :atomics.get(config.generations, @epoch) + :atomics.get(config.generations, stripe(key))

  defp stripe(key), do: 2 + :erlang.phash2(key, @stripes)

  defp config(cache), do: :persistent_term.get({__MODULE__, cache})

  defp now_ms, do: System.monotonic_time(:millisecond)
end
