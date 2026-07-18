defmodule Ingress.Dispatcher.Worker do
  @moduledoc false

  use GenServer

  require Logger

  alias Ingress.{Dispatcher, Trace}

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))
  end

  @impl true
  def init(opts) do
    # Payloads can be large maps and queues are explicitly bounded. Keeping
    # queued messages off-heap prevents each worker GC from repeatedly copying
    # the notification backlog.
    Process.flag(:message_queue_data, :off_heap)

    state = %{
      dispatcher: Keyword.fetch!(opts, :dispatcher),
      index: Keyword.fetch!(opts, :index),
      handler: Keyword.get(opts, :handler, &Ingress.Pipeline.handle_event/2),
      completion_batch_size:
        Keyword.get(
          opts,
          :completion_batch_size,
          Ingress.Config.dispatcher_completion_batch_size()
        ),
      completion_flush_ms:
        Keyword.get(
          opts,
          :completion_flush_ms,
          Ingress.Config.dispatcher_completion_flush_ms()
        ),
      completed_by_broadcaster: %{},
      completed_total: 0
    }

    send(state.dispatcher, {:worker_up, state.index, self()})
    schedule_flush(state.completion_flush_ms)
    {:ok, state}
  end

  @impl true
  def handle_info({:process, enqueued_at, payload, meta}, state) do
    _result =
      try do
        Trace.notification(enqueued_at, payload, meta, fn -> state.handler.(payload, meta) end)
      catch
        kind, reason ->
          Logger.error("Worker pipeline failed: #{kind} #{inspect(reason)}")
      end

    state = record_completion(state, Map.get(meta, :broadcaster_id))
    {:noreply, state}
  end

  def handle_info(:flush_completions, state) do
    state = flush_completions(state)
    schedule_flush(state.completion_flush_ms)
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, state) do
    flush_completions(state)
    :ok
  end

  defp record_completion(state, broadcaster_id) do
    state = %{
      state
      | completed_by_broadcaster:
          Map.update(state.completed_by_broadcaster, broadcaster_id, 1, &(&1 + 1)),
        completed_total: state.completed_total + 1
    }

    if state.completed_total >= state.completion_batch_size do
      flush_completions(state)
    else
      state
    end
  end

  defp flush_completions(%{completed_total: 0} = state), do: state

  defp flush_completions(state) do
    Dispatcher.complete_batch(
      state.dispatcher,
      self(),
      state.completed_by_broadcaster,
      state.completed_total
    )

    %{state | completed_by_broadcaster: %{}, completed_total: 0}
  end

  defp schedule_flush(ms), do: Process.send_after(self(), :flush_completions, ms)
end
