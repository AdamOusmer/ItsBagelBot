# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Drain do
  require Logger

  @draining_key {__MODULE__, :draining}
  @jitter_max_ms 5_000
  @drain_deadline_ms 8_000
  @poll_interval_ms 200

  @spec ready?() :: boolean()
  def ready?, do: not draining?()

  @spec draining?() :: boolean()
  def draining?, do: :persistent_term.get(@draining_key, false)

  @spec track_connection() :: :ok | :draining
  def track_connection do
    {:ok, _} = Registry.register(ControlsGateway.Connections, self(), nil)
    if draining?(), do: :draining, else: :ok
  end

  @doc false
  @spec reset!() :: :ok
  def reset! do
    :persistent_term.erase(@draining_key)
    :ok
  end

  @spec run() :: :ok
  def run do
    :persistent_term.put(@draining_key, true)
    ControlsGateway.Overlays.Puller.pause_all()
    close_all_with_jitter()
    await_drained(deadline())
    :ok
  rescue
    error ->
      Logger.error("drain failed: #{Exception.message(error)}; shutting down anyway")
      :ok
  end

  defp close_all_with_jitter do
    Enum.each(connected_pids(), fn pid ->
      Process.send_after(pid, :shutdown_close, :rand.uniform(@jitter_max_ms))
    end)
  end

  defp connected_pids do
    Registry.select(ControlsGateway.Connections, [{{:_, :"$1", :_}, [], [:"$1"]}])
  end

  defp deadline, do: System.monotonic_time(:millisecond) + @drain_deadline_ms

  defp await_drained(deadline) do
    cond do
      connected_pids() == [] ->
        :ok

      System.monotonic_time(:millisecond) >= deadline ->
        :timeout

      true ->
        Process.sleep(@poll_interval_ms)
        await_drained(deadline)
    end
  end
end
