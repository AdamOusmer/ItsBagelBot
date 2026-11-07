# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Metrics do
  @moduledoc """
  In-process counters only, no New Relic: that would need the new_relic_agent
  dependency, outside this service's allowed dependency list. Exposed as JSON
  at GET /metrics.
  """
  use GenServer

  @table __MODULE__.Counters

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec count(String.t(), integer()) :: :ok
  def count(name, value \\ 1) do
    :ets.update_counter(@table, name, {2, value}, {name, 0})
    :ok
  rescue
    ArgumentError -> :ok
  end

  @spec snapshot() :: %{String.t() => integer()}
  def snapshot do
    Map.new(:ets.tab2list(@table))
  rescue
    ArgumentError -> %{}
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set, write_concurrency: true])
    {:ok, %{}}
  end
end
