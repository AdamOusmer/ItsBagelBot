# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Feed do
  alias ControlsGateway.Overlay.Nats
  alias Platform.Nats.Push

  @stream "OVERLAY_FEED"
  @frames ~w(event stats subathon control)

  def stream, do: @stream

  def start(jetstream, rpc, user_id, last_event_id) do
    case resume_sequence(jetstream, last_event_id) do
      nil -> from_snapshot(rpc, user_id)
      seq -> {:ok, {:sequence, seq}, []}
    end
  end

  def frame(topic, seq, body) do
    with kind when kind in @frames <- topic |> String.split(".") |> List.last(),
         {:ok, %{} = data} <- Jason.decode(body) do
      ["id: ", Integer.to_string(seq), "\nevent: ", kind, "\ndata: ", Jason.encode!(data), "\n\n"]
    else
      _ -> :error
    end
  end

  def chat_frame(body) do
    case Jason.decode(body) do
      {:ok, %{} = line} -> {:ok, ["event: event\ndata: ", Jason.encode!(line), "\n\n"]}
      _ -> :error
    end
  end

  defp resume_sequence(jetstream, last_event_id) do
    with seq when is_integer(seq) <- Push.resume_sequence(last_event_id),
         {:ok, first, last} <- Push.stream_range(jetstream, @stream),
         true <- seq >= first and seq <= last + 1 do
      seq
    else
      _ -> nil
    end
  end

  defp from_snapshot(rpc, user_id) do
    with {:ok, snapshot, seq} <- Nats.snapshot(rpc, user_id) do
      frame = [
        "id: ",
        Integer.to_string(seq),
        "\nevent: snapshot\ndata: ",
        Jason.encode!(snapshot),
        "\n\n"
      ]

      {:ok, {:sequence, seq + 1}, frame}
    end
  end
end
