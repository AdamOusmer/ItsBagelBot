defmodule Ingress.Dispatcher do
  @moduledoc """
  Bounded async notification dispatcher.

  ShardSession owns the websocket socket, so it should only decode enough to
  enqueue work. Filtering, broadcaster cache misses, JSON encoding, and NATS
  publish happen in supervised tasks behind this process.
  """

  use GenServer

  require Logger

  alias Ingress.{Config, Metrics}

  defstruct [
    :max_running,
    :max_queue,
    idle_workers: :queue.new(),
    busy_workers: %{},
    queue: :queue.new()
  ]

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec dispatch(map(), map()) :: :ok
  def dispatch(payload, meta) do
    case Process.whereis(__MODULE__) do
      nil ->
        Metrics.count("Dispatcher/Dropped")
        Logger.warning("ingress dispatcher unavailable; dropping notification")
        :ok

      pid ->
        try do
          count = :ets.update_counter(__MODULE__, :admitted_count, {2, 1})
          capacity = :ets.lookup_element(__MODULE__, :capacity, 2)

          if count > capacity do
            :ets.update_counter(__MODULE__, :admitted_count, {2, -1})
            Metrics.count("Dispatcher/Dropped")
          else
            send(pid, {:dispatch, payload, meta})
          end
        catch
          :error, :badarg ->
            Metrics.count("Dispatcher/Dropped")
        end

        :ok
    end
  end

  @impl true
  def init(opts) do
    max_running = Keyword.get(opts, :max_running, Config.dispatcher_max_running())
    max_queue = Keyword.get(opts, :max_queue, Config.dispatcher_max_queue())
    capacity = max_running + max_queue

    :ets.new(__MODULE__, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true
    ])

    :ets.insert(__MODULE__, {:admitted_count, 0})
    :ets.insert(__MODULE__, {:capacity, capacity})

    {:ok, %__MODULE__{max_running: max_running, max_queue: max_queue}}
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
            busy_workers: Map.put(state.busy_workers, worker_pid, true)
        }

        {:noreply, state}

      {:empty, _} ->
        Metrics.count("Dispatcher/Queued")
        {:noreply, %{state | queue: :queue.in({payload, meta}, state.queue)}}
    end
  end

  def handle_info({:worker_ready, worker_pid}, state) do
    state =
      if Map.has_key?(state.busy_workers, worker_pid) do
        :ets.update_counter(__MODULE__, :admitted_count, {2, -1})
        %{state | busy_workers: Map.delete(state.busy_workers, worker_pid)}
      else
        Process.monitor(worker_pid)
        state
      end

    case :queue.out(state.queue) do
      {{:value, {payload, meta}}, queue} ->
        Metrics.count("Dispatcher/Started")
        GenServer.cast(worker_pid, {:process, payload, meta})

        state = %{
          state
          | queue: queue,
            busy_workers: Map.put(state.busy_workers, worker_pid, true)
        }

        {:noreply, state}

      {:empty, _} ->
        {:noreply, %{state | idle_workers: :queue.in(worker_pid, state.idle_workers)}}
    end
  end

  def handle_info({:DOWN, _ref, :process, pid, _reason}, state) do
    if Map.has_key?(state.busy_workers, pid) do
      :ets.update_counter(__MODULE__, :admitted_count, {2, -1})
      Metrics.count("Dispatcher/TaskFailed")
    end

    state = %{state | busy_workers: Map.delete(state.busy_workers, pid)}

    idle_list = :queue.to_list(state.idle_workers) |> Enum.reject(&(&1 == pid))
    state = %{state | idle_workers: :queue.from_list(idle_list)}

    {:noreply, state}
  end
end
