# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Pending do
  alias ControlsGateway.Metrics
  alias Platform.Nats.Ack

  @max_age_ms 300_000
  @sweep_ms 15_000

  def start(owner) do
    spawn(fn -> loop(Process.monitor(owner), %{}) end)
  end

  def track(pending, ack), do: send(pending, {:track, ack})

  defp loop(ref, acks) do
    receive do
      {:track, ack} ->
        loop(ref, acks |> Map.put(ack, now()) |> prune())

      {:DOWN, ^ref, :process, _pid, reason} ->
        count_client_close(reason)
        Enum.each(Map.keys(acks), &nak/1)
    after
      @sweep_ms -> loop(ref, prune(acks))
    end
  end

  defp count_client_close(:normal), do: :ok
  defp count_client_close(_reason), do: Metrics.count("Stream/Closed/client")

  defp nak(ack) do
    case Ack.nak(:jetstream, ack) do
      :ok -> Metrics.count("Ack/NakOnClose")
      _failed -> Metrics.count("Ack/Failed")
    end
  end

  defp prune(acks) do
    cutoff = now() - @max_age_ms
    Map.reject(acks, fn {_ack, sent_at} -> sent_at < cutoff end)
  end

  defp now, do: System.monotonic_time(:millisecond)
end
