# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Metrics do
  @table __MODULE__.Counters
  @prefix "Custom/Ingress/"
  @drop_totals [
    "Nats/PublishDropped",
    "Trial/AdmissionDropped",
    "Dispatcher/Dropped",
    "Trial/AutomodLaneDropped"
  ]
  @drop_reasons [
    "not_connected",
    "encode_error",
    "overloaded",
    "enqueue_error",
    "unavailable",
    "capacity",
    "broadcaster_cap",
    "publish_failed",
    "crashed"
  ]
  @event_names [
    "Nats/PublishInflight",
    "ShardLoadConcentration",
    "ShardUp",
    "ShardDown",
    "Dispatcher/Workers"
  ]
  @event_attributes [
    :pending,
    :max_pending,
    :utilization_pct,
    :queued,
    :batch_size,
    :batches_inflight,
    :max_load,
    :avg_load,
    :responsive_count,
    :active,
    :admitted
  ]
  @event_policy @event_attributes
                |> Map.new(&{&1, :number})
                |> Map.put(:name, {:enum, @event_names})

  def child_spec(opts), do: %{id: __MODULE__, start: {__MODULE__, :start_link, [opts]}}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    Platform.Metrics.start_link(
      name: @table,
      prefix: @prefix,
      flush_ms: Keyword.get(opts, :flush_ms, 1_000),
      reporter: &reporter/0
    )
  end

  @spec count(String.t(), integer()) :: :ok
  def count(name, value \\ 1), do: Platform.Metrics.count(@table, name, value)

  @spec count_drop(String.t(), atom() | String.t()) :: :ok
  def count_drop(total, reason) do
    count(total)
    reason = to_string(reason)

    if drop_reason?(total, reason), do: count("#{total}/#{reason}"), else: :ok
  end

  @spec event(String.t(), map()) :: :ok
  def event(name, attributes \\ %{})

  def event(name, attributes) when name in @event_names do
    safe =
      attributes
      |> Map.take(@event_attributes)
      |> Enum.filter(fn {_, value} -> is_number(value) end)
      |> Map.new()
      |> Map.put(:name, name)

    Platform.NewRelic.event("IngressEvent", safe, @event_policy)
    :ok
  end

  def event(_name, _attributes), do: :ok

  @doc false
  def flush, do: Platform.Metrics.flush(@table)

  defp reporter, do: Application.get_env(:ingress, :metrics_reporter, Platform.NewRelic)

  defp drop_reason?(total, reason), do: total in @drop_totals and reason in @drop_reasons
end
