defmodule Ingress.Dispatcher do
  @moduledoc """
  Bounded async notification dispatcher.

  ShardSession owns the websocket socket, so it should only decode enough to
  enqueue work. Filtering, broadcaster cache misses, JSON encoding, and NATS
  publish happen in supervised tasks behind this process.
  """

  use GenServer

  require Logger

  alias Ingress.{Config, Metrics, Pipeline}

  @task_supervisor Ingress.Dispatcher.TaskSupervisor

  defstruct [:max_running, :max_queue, running: %{}, queue: :queue.new()]

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
        send(pid, {:dispatch, payload, meta})
        :ok
    end
  end

  @impl true
  def init(opts) do
    max_running = Keyword.get(opts, :max_running, Config.dispatcher_max_running())
    max_queue = Keyword.get(opts, :max_queue, Config.dispatcher_max_queue())

    {:ok, %__MODULE__{max_running: max_running, max_queue: max_queue}}
  end

  @impl true
  def handle_info({:dispatch, payload, meta}, state) do
    enqueue_or_start({payload, meta}, state)
  end

  def handle_info({ref, _result}, state) when is_reference(ref) do
    Process.demonitor(ref, [:flush])
    finish(ref, state)
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, state) do
    if reason != :normal do
      Logger.warning("ingress dispatch task failed: #{inspect(reason)}")
      Metrics.count("Dispatcher/TaskFailed")
    end

    finish(ref, state)
  end

  defp enqueue_or_start(item, state) do
    if map_size(state.running) < state.max_running do
      {:noreply, start_task(item, state)}
    else
      if :queue.len(state.queue) < state.max_queue do
        Metrics.count("Dispatcher/Queued")
        {:noreply, %{state | queue: :queue.in(item, state.queue)}}
      else
        Metrics.count("Dispatcher/Dropped")
        {:noreply, state}
      end
    end
  end

  defp finish(ref, state) do
    state = %{state | running: Map.delete(state.running, ref)}

    case :queue.out(state.queue) do
      {{:value, item}, queue} ->
        {:noreply, start_task(item, %{state | queue: queue})}

      {:empty, _queue} ->
        {:noreply, state}
    end
  end

  defp start_task({payload, meta}, state) do
    task =
      Task.Supervisor.async_nolink(@task_supervisor, fn ->
        Pipeline.handle_event(payload, meta)
      end)

    Metrics.count("Dispatcher/Started")
    %{state | running: Map.put(state.running, task.ref, task.pid)}
  end
end
