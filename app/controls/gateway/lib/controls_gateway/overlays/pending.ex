# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Pending do
  alias ControlsGateway.Metrics
  alias Platform.Nats.Ack

  @max_age_ms 300_000
  @sweep_ms 15_000
  @close_metrics %{nak: "Ack/NakOnClose", progress: "Ack/ProgressOnHandoff"}

  def start(owner) do
    spawn(fn -> loop(Process.monitor(owner), %{}, :open) end)
  end

  def track(pending, ack), do: send(pending, {:track, ack})

  def handoff(pending), do: send(pending, :handoff)

  defp loop(ref, acks, mode) do
    receive do
      {:track, ack} ->
        loop(ref, acks |> Map.put(ack, now()) |> prune(), mode)

      :handoff ->
        loop(ref, acks, :handoff)

      {:DOWN, ^ref, :process, _pid, reason} ->
        count_client_close(reason)
        action = close_action(mode, reason)
        Enum.each(Map.keys(acks), &release(&1, action))
    after
      @sweep_ms -> loop(ref, prune(acks), mode)
    end
  end

  defp close_action(:handoff, reason) when reason != :normal, do: :progress
  defp close_action(_mode, _reason), do: :nak

  defp count_client_close(:normal), do: :ok
  defp count_client_close(_reason), do: Metrics.count("Stream/Closed/client")

  defp release(ack, action) do
    case publish(action, ack) do
      :ok -> Metrics.count(Map.fetch!(@close_metrics, action))
      _failed -> Metrics.count("Ack/Failed")
    end
  end

  defp publish(:nak, ack), do: Ack.nak(:jetstream, ack)
  defp publish(:progress, ack), do: Ack.progress(:jetstream, ack)

  defp prune(acks) do
    cutoff = now() - @max_age_ms
    Map.reject(acks, fn {_ack, sent_at} -> sent_at < cutoff end)
  end

  defp now, do: System.monotonic_time(:millisecond)
end
