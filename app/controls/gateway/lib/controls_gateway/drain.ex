# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Drain do
  @moduledoc """
  SIGTERM handling: fail /readyz first, reject new /v1/connect upgrades at
  the handler (the shared listener keeps serving /healthz and /readyz), then
  close open sockets with "going away" plus jitter so clients don't all
  reconnect in the same instant.
  """
  require Logger

  @draining_key {__MODULE__, :draining}
  @jitter_max_ms 5_000
  @drain_deadline_ms 8_000
  @poll_interval_ms 200

  @spec ready?() :: boolean()
  def ready?, do: not draining?()

  @spec draining?() :: boolean()
  def draining?, do: :persistent_term.get(@draining_key, false)

  # Test-only counterpart to run/0: draining is permanent for a real process
  # (it is shutting down anyway), but the flag is a VM-wide persistent_term
  # and must not leak into the next test.
  @doc false
  @spec reset!() :: :ok
  def reset! do
    :persistent_term.erase(@draining_key)
    :ok
  end

  @spec run() :: :ok
  def run do
    :persistent_term.put(@draining_key, true)
    close_all_with_jitter()
    await_drained(deadline())
    :ok
  rescue
    error ->
      Logger.error("drain failed: #{Exception.message(error)}; shutting down anyway")
      :ok
  end

  defp close_all_with_jitter do
    for pid <- connected_pids() do
      send(pid, {:shutdown_close, :rand.uniform(@jitter_max_ms)})
    end
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
