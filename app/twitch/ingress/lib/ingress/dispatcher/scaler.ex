# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Dispatcher.Scaler do
  @moduledoc false

  use GenServer

  alias Ingress.Config.Dispatcher, as: DispatcherConfig
  alias Ingress.{Dispatcher, Metrics}
  alias Ingress.Dispatcher.WorkerSupervisor

  @backlog_per_worker 4

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :scaler_name, __MODULE__))
  end

  @impl true
  def init(opts) do
    {floor, ceiling} = DispatcherConfig.pool_bounds(opts)
    dispatcher = Keyword.get(opts, :name, Dispatcher)
    now = now_ms()
    Dispatcher.set_active_workers(dispatcher, floor)

    state = %{
      opts: opts,
      dispatcher: dispatcher,
      floor: floor,
      ceiling: ceiling,
      interval_ms: Keyword.get(opts, :scale_interval_ms, DispatcherConfig.scale_interval_ms()),
      shrink_after_ms: Keyword.get(opts, :shrink_after_ms, DispatcherConfig.shrink_after_ms()),
      retire_grace_ms: Keyword.get(opts, :retire_grace_ms, DispatcherConfig.retire_grace_ms()),
      peak: floor,
      window_started_ms: now,
      retiring: adopt_surplus(opts, floor, now)
    }

    schedule(state.interval_ms)
    {:ok, state}
  end

  @impl true
  def handle_info(:scale, state) do
    now = now_ms()
    state = state |> scale(now) |> retire(now)
    schedule(state.interval_ms)
    {:noreply, state}
  end

  defp adopt_surplus(opts, floor, now) do
    for index <- WorkerSupervisor.running_indices(opts), index >= floor, into: %{} do
      {index, now}
    end
  end

  defp scale(state, now) do
    wanted = wanted_workers(state)
    active = Dispatcher.active_workers(state.dispatcher)
    peak = max(state.peak, wanted)

    cond do
      wanted > active ->
        grow(state, active, wanted, now)

      now - state.window_started_ms >= state.shrink_after_ms ->
        close_window(state, active, peak, now)

      true ->
        %{state | peak: peak}
    end
  end

  defp wanted_workers(state) do
    state.dispatcher
    |> Dispatcher.admitted_count()
    |> Kernel.+(@backlog_per_worker - 1)
    |> div(@backlog_per_worker)
    |> max(state.floor)
    |> min(state.ceiling)
  end

  defp grow(state, active, wanted, now) do
    indices = active..(wanted - 1)
    Enum.each(indices, &(:ok = WorkerSupervisor.start_worker(state.opts, &1)))
    Dispatcher.set_active_workers(state.dispatcher, wanted)
    report(state, wanted)

    %{
      state
      | peak: wanted,
        window_started_ms: now,
        retiring: Map.drop(state.retiring, Enum.to_list(indices))
    }
  end

  defp close_window(state, active, peak, now) when peak < active do
    Dispatcher.set_active_workers(state.dispatcher, peak)
    report(state, peak)

    retiring = for index <- peak..(active - 1), into: state.retiring, do: {index, now}
    %{state | peak: state.floor, window_started_ms: now, retiring: retiring}
  end

  defp close_window(state, _active, _peak, now) do
    %{state | peak: state.floor, window_started_ms: now}
  end

  # A dispatch that read the old active count can still target a retiring worker until the grace ends.
  defp retire(state, now) do
    retired =
      for {index, retired_at} <- state.retiring,
          now - retired_at >= state.retire_grace_ms,
          not Dispatcher.worker_in_flight?(state.dispatcher, index) do
        :ok = WorkerSupervisor.stop_worker(state.opts, index)
        index
      end

    %{state | retiring: Map.drop(state.retiring, retired)}
  end

  defp report(state, active) do
    Metrics.event("Dispatcher/Workers", %{
      active: active,
      admitted: Dispatcher.admitted_count(state.dispatcher)
    })
  end

  defp schedule(interval_ms), do: Process.send_after(self(), :scale, interval_ms)

  defp now_ms, do: System.monotonic_time(:millisecond)
end
