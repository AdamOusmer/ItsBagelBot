# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Metrics do
  use GenServer

  @table __MODULE__.Counters
  @flush_ms 1_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @spec count(String.t(), integer()) :: :ok
  def count(name, value \\ 1) do
    :ets.update_counter(@table, name, {2, value}, {name, 0})
    :ok
  rescue
    ArgumentError -> :ok
  end

  @spec count_drop(String.t(), atom() | String.t()) :: :ok
  def count_drop(total, reason) do
    count(total)
    reason = to_string(reason)

    if total in [
         "Nats/PublishDropped",
         "Trial/AdmissionDropped",
         "Dispatcher/Dropped",
         "Trial/AutomodLaneDropped"
       ] and
         reason in [
           "not_connected",
           "encode_error",
           "overloaded",
           "enqueue_error",
           "unavailable",
           "capacity",
           "broadcaster_cap",
           "publish_failed",
           "crashed"
         ] do
      count("#{total}/#{reason}")
    end
  end

  @spec event(String.t(), map()) :: :ok
  def event(name, attributes \\ %{}) do
    if name in [
         "Nats/PublishInflight",
         "ShardLoadConcentration",
         "ShardUp",
         "ShardDown",
         "Dispatcher/Workers"
       ] do
      allowed = [
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

      safe =
        Map.take(attributes, allowed)
        |> Enum.filter(fn {_, value} -> is_number(value) end)
        |> Map.new()
        |> Map.put(:name, name)

      GenServer.cast(__MODULE__, {:event, safe})
    end

    :ok
  end

  @doc false
  def flush, do: GenServer.call(__MODULE__, :flush)

  @impl true
  def init(opts) do
    :ets.new(@table, [
      :named_table,
      :public,
      :set,
      write_concurrency: true,
      decentralized_counters: true
    ])

    flush_ms = Keyword.get(opts, :flush_ms, @flush_ms)
    schedule(flush_ms)
    {:ok, %{flush_ms: flush_ms}}
  end

  @impl true
  def handle_call(:flush, _from, state) do
    flush_counters()
    {:reply, :ok, state}
  end

  @impl true
  def handle_cast({:event, attrs}, state) do
    policy =
      Map.new(
        [
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
        ],
        &{&1, :number}
      )
      |> Map.put(
        :name,
        {:enum,
         [
           "Nats/PublishInflight",
           "ShardLoadConcentration",
           "ShardUp",
           "ShardDown",
           "Dispatcher/Workers"
         ]}
      )

    Platform.NewRelic.event("IngressEvent", attrs, policy)
    {:noreply, state}
  end

  @impl true
  def handle_info(:flush, state) do
    flush_counters()
    schedule(state.flush_ms)
    {:noreply, state}
  end

  defp flush_counters do
    for {name, value} <- :ets.tab2list(@table), value != 0 do
      :ets.update_counter(@table, name, {2, -value})

      unless report_counter(name, value) do
        :ets.update_counter(@table, name, {2, value})
      end
    end
  end

  defp report_counter(name, value) do
    Application.get_env(:ingress, :metrics_reporter, Platform.NewRelic).increment(
      "Custom/Ingress/" <> name,
      value
    ) == :ok
  rescue
    _ -> false
  catch
    _, _ -> false
  end

  defp schedule(ms), do: Process.send_after(self(), :flush, ms)
end
