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
    count("#{total}/#{reason}")
  end

  @spec event(String.t(), map()) :: :ok
  def event(name, attributes \\ %{}) do
    NewRelic.report_custom_event(
      "IngressEvent",
      Map.merge(%{name: name, node: to_string(node())}, attributes)
    )

    :ok
  rescue
    _exception -> :ok
  catch
    _kind, _reason -> :ok
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
    NewRelic.increment_custom_metric("Custom/Ingress/" <> name, value)
    true
  rescue
    _exception -> false
  catch
    _kind, _reason -> false
  end

  defp schedule(ms), do: Process.send_after(self(), :flush, ms)
end
