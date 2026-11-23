# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Metrics do
  @moduledoc "Gateway counters, flushed to New Relic by Platform.Metrics."

  @table __MODULE__
  @prefix "Custom/ControlsGateway/"
  @pull_events Enum.map([:attach, :detach, :pause, :status], &[:platform, :nats, :pull, &1])
  @cache_events Enum.map([:hit, :miss, :stale, :invalidated, :gap], &[:platform, :cache, &1])

  def child_spec(_opts) do
    Platform.Metrics.child_spec(
      name: @table,
      prefix: @prefix,
      flush_ms: Application.get_env(:controls_gateway, :metrics_flush_ms, 1_000),
      reporter: Application.get_env(:controls_gateway, :metrics_reporter, Platform.NewRelic)
    )
  end

  @spec count(String.t(), integer()) :: :ok
  def count(name, value \\ 1), do: Platform.Metrics.count(@table, name, value)

  def attach_pull do
    :telemetry.attach_many("controls-gateway-pull", @pull_events, &__MODULE__.pull_event/4, nil)
  end

  def attach_cache do
    :telemetry.attach_many(
      "controls-gateway-cache",
      @cache_events,
      &__MODULE__.cache_event/4,
      nil
    )
  end

  @doc false
  def cache_event([:platform, :cache, event], _measurements, %{name: name}, _config),
    do: count("Cache/#{Macro.camelize(Atom.to_string(event))}/#{name}")

  @doc false
  def pull_event([:platform, :nats, :pull, event], _measurements, metadata, _config),
    do: count(pull_metric(event, metadata))

  defp pull_metric(:attach, _metadata), do: "Pull/Attach"
  defp pull_metric(:detach, _metadata), do: "Pull/Detached"
  defp pull_metric(:pause, _metadata), do: "Pull/Paused"
  defp pull_metric(:status, %{status: status}), do: "Pull/Status/#{status}"
end
