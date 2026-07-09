defmodule Ingress.Dispatcher do
  @moduledoc """
  Bounded async notification dispatcher.

  ShardSession owns the websocket socket, so it should only decode enough to
  enqueue work. Filtering, broadcaster cache misses, JSON encoding, and NATS
  publish happen in supervised tasks behind this process.

  Capacity (`max_running` + `max_queue`) is one shared pool per pod, not per
  shard — a node can host more than one `ShardSession`. A second, per-broadcaster
  ETS counter (`max_per_broadcaster`) caps how much of that shared pool one
  broadcaster can consume at once, so a hot channel can't starve every other
  broadcaster sharing the pod. Dead per-broadcaster counters are swept
  periodically. `:name` is overridable for test isolation.
  """

  use GenServer

  require Logger

  alias Ingress.{Config, Metrics}

  defstruct [
    :name,
    :max_running,
    :max_queue,
    :sweep_ms,
    idle_workers: :queue.new(),
    busy_workers: %{},
    queue: :queue.new()
  ]

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @spec dispatch(map(), map(), GenServer.server()) :: :ok
  def dispatch(payload, meta, server \\ __MODULE__) do
    case Process.whereis(server) do
      nil ->
        Logger.warning("ingress dispatcher unavailable; dropping notification")
        drop(meta, "unavailable")

      pid ->
        admit(pid, server, payload, meta)
    end

    :ok
  end

  defp admit(pid, server, payload, meta) do
    broadcaster_id = Map.get(meta, :broadcaster_id)

    try do
      if broadcaster_admitted?(server, broadcaster_id, meta) do
        count = :ets.update_counter(server, :admitted_count, {2, 1})
        capacity = :ets.lookup_element(server, :capacity, 2)

        if count > capacity do
          :ets.update_counter(server, :admitted_count, {2, -1})
          release_broadcaster_slot(server, broadcaster_id)
          drop(meta, "capacity")
        else
          send(pid, {:dispatch, payload, meta})
        end
      end
    catch
      :error, :badarg -> drop(meta, "unavailable")
    end
  end

  # No broadcaster to attribute (malformed/unrecognized event shape): fail open,
  # only the pod-wide capacity check applies.
  defp broadcaster_admitted?(_server, nil, _meta), do: true

  defp broadcaster_admitted?(server, broadcaster_id, meta) do
    count =
      :ets.update_counter(server, {:bc, broadcaster_id}, {2, 1}, {{:bc, broadcaster_id}, 0})

    max_per_bc = :ets.lookup_element(server, :max_per_broadcaster, 2)

    if count > max_per_bc do
      :ets.update_counter(server, {:bc, broadcaster_id}, {2, -1})
      drop(meta, "broadcaster_cap")
      false
    else
      true
    end
  end

  defp release_broadcaster_slot(_server, nil), do: :ok

  defp release_broadcaster_slot(server, broadcaster_id) do
    :ets.update_counter(server, {:bc, broadcaster_id}, {2, -1})
  end

  defp drop(meta, reason) do
    Metrics.count("Dispatcher/Dropped")

    Metrics.event("Dispatcher/Dropped", %{
      reason: reason,
      shard_id: Map.get(meta, :shard_id),
      broadcaster_id: Map.get(meta, :broadcaster_id)
    })
  end

  @impl true
  def init(opts) do
    name = Keyword.get(opts, :name, __MODULE__)
    max_running = Keyword.get(opts, :max_running, Config.dispatcher_max_running())
    max_queue = Keyword.get(opts, :max_queue, Config.dispatcher_max_queue())

    max_per_broadcaster =
      Keyword.get(opts, :max_per_broadcaster, Config.dispatcher_max_per_broadcaster())

    sweep_ms = Keyword.get(opts, :sweep_ms, Config.dispatcher_broadcaster_sweep_ms())
    capacity = max_running + max_queue

    :ets.new(name, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true
    ])

    :ets.insert(name, {:admitted_count, 0})
    :ets.insert(name, {:capacity, capacity})
    :ets.insert(name, {:max_per_broadcaster, max_per_broadcaster})

    schedule_sweep(sweep_ms)

    {:ok,
     %__MODULE__{
       name: name,
       max_running: max_running,
       max_queue: max_queue,
       sweep_ms: sweep_ms
     }}
  end

  @impl true
  def handle_info({:dispatch, payload, meta}, state) do
    case :queue.out(state.idle_workers) do
      {{:value, worker_pid}, idle_workers} ->
        Metrics.count("Dispatcher/Started")
        GenServer.cast(worker_pid, {:process, payload, meta})

        state = %{
          state
          | idle_workers: idle_workers,
            busy_workers: Map.put(state.busy_workers, worker_pid, meta)
        }

        {:noreply, state}

      {:empty, _} ->
        Metrics.count("Dispatcher/Queued")
        {:noreply, %{state | queue: :queue.in({payload, meta}, state.queue)}}
    end
  end

  def handle_info({:worker_ready, worker_pid}, state) do
    state =
      case Map.pop(state.busy_workers, worker_pid) do
        {nil, busy_workers} ->
          Process.monitor(worker_pid)
          %{state | busy_workers: busy_workers}

        {meta, busy_workers} ->
          :ets.update_counter(state.name, :admitted_count, {2, -1})
          release_broadcaster_slot(state.name, Map.get(meta, :broadcaster_id))
          %{state | busy_workers: busy_workers}
      end

    case :queue.out(state.queue) do
      {{:value, {payload, meta}}, queue} ->
        Metrics.count("Dispatcher/Started")
        GenServer.cast(worker_pid, {:process, payload, meta})

        state = %{
          state
          | queue: queue,
            busy_workers: Map.put(state.busy_workers, worker_pid, meta)
        }

        {:noreply, state}

      {:empty, _} ->
        {:noreply, %{state | idle_workers: :queue.in(worker_pid, state.idle_workers)}}
    end
  end

  def handle_info({:DOWN, _ref, :process, pid, _reason}, state) do
    state =
      case Map.pop(state.busy_workers, pid) do
        {nil, busy_workers} ->
          %{state | busy_workers: busy_workers}

        {meta, busy_workers} ->
          :ets.update_counter(state.name, :admitted_count, {2, -1})
          release_broadcaster_slot(state.name, Map.get(meta, :broadcaster_id))

          Metrics.count("Dispatcher/TaskFailed")

          Metrics.event("Dispatcher/TaskFailed", %{
            shard_id: Map.get(meta, :shard_id),
            broadcaster_id: Map.get(meta, :broadcaster_id)
          })

          %{state | busy_workers: busy_workers}
      end

    idle_list = :queue.to_list(state.idle_workers) |> Enum.reject(&(&1 == pid))
    state = %{state | idle_workers: :queue.from_list(idle_list)}

    {:noreply, state}
  end

  def handle_info(:sweep, state) do
    :ets.select_delete(state.name, [{{{:bc, :"$1"}, 0}, [], [true]}])
    schedule_sweep(state.sweep_ms)
    {:noreply, state}
  end

  defp schedule_sweep(sweep_ms), do: Process.send_after(self(), :sweep, sweep_ms)
end
