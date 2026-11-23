# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Drain do
  alias ControlsGateway.Metrics

  @draining_key {__MODULE__, :draining}
  @registry ControlsGateway.Connections
  @socket_jitter_max_ms 5_000
  @reconnect_max_ms 1_500
  @drain_deadline_ms 25_000
  @poll_interval_ms 200

  @spec ready?() :: boolean()
  def ready?, do: not draining?()

  @spec draining?() :: boolean()
  def draining?, do: :persistent_term.get(@draining_key, false)

  @spec track_connection(:stream | :socket | :download) :: :ok | :draining
  def track_connection(kind) do
    {:ok, _} = Registry.register(@registry, self(), kind)
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
    started = now()
    :persistent_term.put(@draining_key, true)
    ControlsGateway.Overlays.pause_pullers()
    ControlsGateway.Overlays.await_idle()
    Enum.each(connections(), &announce/1)
    await_drained(started + deadline_ms())
    Metrics.count("Drain/DurationMs", now() - started)
    Metrics.count("Drain/StreamsLeft", count(:stream))
    Platform.Metrics.flush(Metrics)
    :ok
  end

  defp announce({pid, :stream}),
    do: send(pid, {:reconnect, Platform.Backoff.between_ms(0, @reconnect_max_ms), :drain})

  defp announce({pid, :socket}),
    do:
      Process.send_after(pid, :shutdown_close, Platform.Backoff.uniform_ms(@socket_jitter_max_ms))

  defp announce({_pid, :download}), do: :ok

  defp connections do
    Registry.select(@registry, [{{:_, :"$1", :"$2"}, [], [{{:"$1", :"$2"}}]}])
  end

  defp deadline_ms,
    do: Application.get_env(:controls_gateway, :drain_deadline_ms, @drain_deadline_ms)

  defp now, do: System.monotonic_time(:millisecond)

  defp count(kind), do: Enum.count(connections(), &match?({_pid, ^kind}, &1))

  defp await_drained(deadline) do
    cond do
      connections() == [] ->
        :ok

      now() >= deadline ->
        :timeout

      true ->
        Process.sleep(@poll_interval_ms)
        await_drained(deadline)
    end
  end
end
