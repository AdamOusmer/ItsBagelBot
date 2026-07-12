defmodule Ingress.Dispatcher do
  @moduledoc """
  Scheduler-local, bounded notification dispatch.

  The websocket process performs admission directly in a public ETS table and
  sends accepted work straight to one of a fixed set of supervised workers.
  There is deliberately no central queue process on the event path: worker
  mailboxes are the queues, and a caller-local round-robin cursor
  spreads calls across them without forcing every shard through one mailbox.

  The GenServer that owns the table only performs cold-path housekeeping:
  monitoring workers so admission slots can be reclaimed after a crash and
  deleting zeroed counters. Pod-wide admission uses atomics, and workers fold
  completion bookkeeping into small bounded batches. Per-broadcaster limits
  and worker-crash reclamation remain exact.
  """

  use GenServer

  alias Ingress.{Config, Metrics}

  defstruct [:name, :table, :admitted, :sweep_ms, worker_refs: %{}]

  @type context :: %{
          table: atom(),
          admitted: reference(),
          capacity: pos_integer(),
          max_per_broadcaster: pos_integer(),
          workers: tuple(),
          worker_count: pos_integer()
        }

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @spec dispatch(map(), map(), GenServer.server()) :: :ok
  def dispatch(payload, meta, server \\ __MODULE__) do
    case context(server) do
      nil ->
        drop(meta, "unavailable")

      ctx ->
        admit_and_send(ctx, payload, meta)
    end

    :ok
  end

  @doc false
  @spec complete(GenServer.server(), pid(), String.t() | nil) :: :ok
  def complete(server, worker, broadcaster_id) do
    complete_batch(server, worker, %{broadcaster_id => 1}, 1)
  end

  @doc false
  @spec complete_batch(GenServer.server(), pid(), map(), pos_integer()) :: :ok
  def complete_batch(server, worker, completed_by_broadcaster, total) do
    case context(server) do
      nil ->
        :ok

      ctx ->
        Enum.each(completed_by_broadcaster, fn {broadcaster_id, count} ->
          :ets.update_counter(ctx.table, {:worker, worker, broadcaster_id}, {2, -count})
          release_broadcaster_slot(ctx.table, broadcaster_id, count)
        end)

        :atomics.sub(ctx.admitted, 1, total)
        :ok
    end
  rescue
    ArgumentError -> :ok
  end

  @doc false
  def admitted_count(server \\ __MODULE__) do
    case context(server) do
      nil -> 0
      ctx -> :atomics.get(ctx.admitted, 1)
    end
  end

  @doc false
  def worker_names(server) do
    case context(server) do
      nil -> {}
      %{workers: workers} -> workers
    end
  end

  defp context(server), do: :persistent_term.get({__MODULE__, :ctx, server}, nil)

  defp admit_and_send(ctx, payload, meta) do
    broadcaster_id = Map.get(meta, :broadcaster_id)

    try do
      if broadcaster_admitted?(ctx, broadcaster_id, meta) do
        count = :atomics.add_get(ctx.admitted, 1, 1)

        if count > ctx.capacity do
          :atomics.sub(ctx.admitted, 1, 1)
          release_broadcaster_slot(ctx.table, broadcaster_id, 1)
          drop(meta, "capacity")
        else
          send_to_worker(ctx, payload, meta, broadcaster_id)
        end
      end
    catch
      :error, :badarg -> drop(meta, "unavailable")
    end
  end

  defp send_to_worker(ctx, payload, meta, broadcaster_id) do
    scheduler = max(:erlang.system_info(:scheduler_id), 1)
    cursor_key = {__MODULE__, :cursor, ctx.table}
    ticket = Process.get(cursor_key, scheduler - 1)
    Process.put(cursor_key, ticket + 1)

    # Each websocket producer owns its cursor, so choosing a worker requires no
    # shared counter at all and remains balanced if the process migrates between
    # schedulers. The scheduler id only offsets each producer's starting point.
    index = rem(ticket, ctx.worker_count)
    worker_name = elem(ctx.workers, index)

    case Process.whereis(worker_name) do
      nil ->
        release(ctx, nil, broadcaster_id, 1)
        drop(meta, "unavailable")

      worker ->
        :ets.update_counter(
          ctx.table,
          {:worker, worker, broadcaster_id},
          {2, 1},
          {{:worker, worker, broadcaster_id}, 0}
        )

        # Close the only meaningful death race: if the worker went down after
        # whereis/1 but before attribution was recorded, its DOWN cleanup may
        # already have run. In that case reclaim this event here.
        if Process.alive?(worker) do
          send(worker, {:process, payload, meta})
        else
          release(ctx, worker, broadcaster_id, 1)
          drop(meta, "unavailable")
        end
    end
  end

  defp broadcaster_admitted?(_ctx, nil, _meta), do: true

  defp broadcaster_admitted?(ctx, broadcaster_id, meta) do
    count =
      :ets.update_counter(
        ctx.table,
        {:bc, broadcaster_id},
        {2, 1},
        {{:bc, broadcaster_id}, 0}
      )

    if count > ctx.max_per_broadcaster do
      :ets.update_counter(ctx.table, {:bc, broadcaster_id}, {2, -1})
      drop(meta, "broadcaster_cap")
      false
    else
      true
    end
  end

  defp release(ctx, worker, broadcaster_id, count) do
    if worker do
      :ets.update_counter(ctx.table, {:worker, worker, broadcaster_id}, {2, -count})
    end

    :atomics.sub(ctx.admitted, 1, count)
    release_broadcaster_slot(ctx.table, broadcaster_id, count)
    :ok
  rescue
    ArgumentError -> :ok
  end

  defp release_broadcaster_slot(_table, nil, _count), do: :ok

  defp release_broadcaster_slot(table, broadcaster_id, count) do
    :ets.update_counter(table, {:bc, broadcaster_id}, {2, -count})
  end

  defp drop(_meta, reason) do
    Metrics.count("Dispatcher/Dropped")
    Metrics.count("Dispatcher/Dropped/#{reason}")
  end

  @impl true
  def init(opts) do
    name = Keyword.get(opts, :name, __MODULE__)
    table = Keyword.get(opts, :table, name)
    worker_count = Keyword.get(opts, :max_running, Config.dispatcher_max_running())
    max_queue = Keyword.get(opts, :max_queue, Config.dispatcher_max_queue())

    max_per_broadcaster =
      Keyword.get(opts, :max_per_broadcaster, Config.dispatcher_max_per_broadcaster())

    sweep_ms = Keyword.get(opts, :sweep_ms, Config.dispatcher_broadcaster_sweep_ms())
    capacity = worker_count + max_queue
    workers = worker_names_tuple(name, worker_count)
    admitted = :atomics.new(1, signed: true)

    :ets.new(table, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true,
      decentralized_counters: true
    ])

    :persistent_term.put(
      {__MODULE__, :ctx, name},
      %{
        table: table,
        admitted: admitted,
        capacity: capacity,
        max_per_broadcaster: max_per_broadcaster,
        workers: workers,
        worker_count: worker_count
      }
    )

    schedule_sweep(sweep_ms)
    {:ok, %__MODULE__{name: name, table: table, admitted: admitted, sweep_ms: sweep_ms}}
  end

  @impl true
  def handle_info({:worker_up, index, pid}, state) do
    ref = Process.monitor(pid)
    {:noreply, %{state | worker_refs: Map.put(state.worker_refs, ref, {index, pid})}}
  end

  def handle_info({:DOWN, ref, :process, pid, _reason}, state) do
    {_worker, worker_refs} = Map.pop(state.worker_refs, ref)
    reclaim_worker(state.table, pid, state.admitted)
    {:noreply, %{state | worker_refs: worker_refs}}
  end

  def handle_info(:sweep, state) do
    :ets.select_delete(state.table, [{{{:bc, :_}, 0}, [], [true]}])
    :ets.select_delete(state.table, [{{{:worker, :_, :_}, 0}, [], [true]}])
    schedule_sweep(state.sweep_ms)
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, state) do
    :persistent_term.erase({__MODULE__, :ctx, state.name})
    :ok
  end

  defp reclaim_worker(table, pid, admitted) do
    match = [{{{:worker, pid, :"$1"}, :"$2"}, [{:>, :"$2", 0}], [:"$1"]}]

    for broadcaster_id <- :ets.select(table, match) do
      key = {:worker, pid, broadcaster_id}

      case :ets.take(table, key) do
        [{^key, count}] when count > 0 ->
          :atomics.sub(admitted, 1, count)
          release_broadcaster_slot(table, broadcaster_id, count)

        _ ->
          :ok
      end
    end
  rescue
    ArgumentError -> :ok
  end

  defp worker_names_tuple(server, count) do
    0..(count - 1)
    |> Enum.map(fn index -> String.to_atom("#{server}.Worker.#{index}") end)
    |> List.to_tuple()
  end

  defp schedule_sweep(sweep_ms), do: Process.send_after(self(), :sweep, sweep_ms)
end
