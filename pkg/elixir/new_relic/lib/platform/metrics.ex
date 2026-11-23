# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Metrics do
  @moduledoc "Hot-path ETS counters flushed periodically through a reporter."
  use GenServer

  @default_flush_ms 1_000

  def child_spec(opts) do
    %{id: Keyword.fetch!(opts, :name), start: {__MODULE__, :start_link, [opts]}}
  end

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))
  end

  def count(table, metric, value \\ 1) do
    :ets.update_counter(table, metric, {2, value}, {metric, 0})
    :ok
  rescue
    ArgumentError -> :ok
  end

  def flush(server), do: GenServer.call(server, :flush)

  def snapshot(server), do: server |> :ets.tab2list() |> Map.new()

  @impl true
  def init(opts) do
    table = Keyword.fetch!(opts, :name)

    :ets.new(table, [
      :named_table,
      :public,
      :set,
      write_concurrency: true,
      decentralized_counters: true
    ])

    state = %{
      table: table,
      prefix: Keyword.fetch!(opts, :prefix),
      flush_ms: Keyword.get(opts, :flush_ms, @default_flush_ms),
      reporter: Keyword.get(opts, :reporter, Platform.NewRelic)
    }

    schedule(state.flush_ms)
    {:ok, state}
  end

  @impl true
  def handle_call(:flush, _from, state) do
    flush_counters(state)
    {:reply, :ok, state}
  end

  @impl true
  def handle_info(:flush, state) do
    flush_counters(state)
    schedule(state.flush_ms)
    {:noreply, state}
  end

  defp flush_counters(state) do
    for {metric, value} <- :ets.tab2list(state.table), value != 0 do
      :ets.update_counter(state.table, metric, {2, -value})

      unless report(state, metric, value) do
        :ets.update_counter(state.table, metric, {2, value})
      end
    end
  end

  defp report(state, metric, value) do
    reporter(state.reporter).increment(state.prefix <> metric, value) == :ok
  rescue
    _ -> false
  catch
    _, _ -> false
  end

  defp reporter(fun) when is_function(fun, 0), do: fun.()
  defp reporter(module), do: module

  defp schedule(ms), do: Process.send_after(self(), :flush, ms)
end
