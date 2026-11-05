# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AutomodTrialLane do
  use GenServer

  alias Ingress.{Config, Metrics, Nats, Pipeline}

  @max_queue 2048

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec publish(map(), map()) :: :ok
  def publish(payload, meta) do
    case Config.automod_trial_subject() do
      subject when is_binary(subject) ->
        enqueue({:copy, subject, payload, meta, DateTime.utc_now()})

      _unset ->
        :ok
    end
  end

  @impl true
  def init(nil), do: {:ok, nil}

  @impl true
  def handle_info({:copy, subject, payload, meta, ingested_at}, state) do
    copy(subject, payload, meta, ingested_at)
    {:noreply, state}
  end

  defp enqueue(message) do
    if has_room?(Process.whereis(__MODULE__)) do
      send(__MODULE__, message)
    else
      drop("overloaded")
    end

    :ok
  end

  defp has_room?(nil), do: false

  defp has_room?(pid) do
    case Process.info(pid, :message_queue_len) do
      {:message_queue_len, n} -> n < @max_queue
      nil -> false
    end
  end

  defp copy(subject, payload, meta, ingested_at) do
    with input when is_map(input) <- Pipeline.trial_chat_input(payload, meta, ingested_at),
         :ok <- Nats.publish(subject, input) do
      :ok
    else
      nil -> :ok
      _refused -> drop("publish_failed")
    end
  catch
    _kind, _reason -> drop("crashed")
  end

  defp drop(reason), do: Metrics.count_drop("Trial/AutomodLaneDropped", reason)
end
