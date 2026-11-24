# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Feed do
  alias ControlsGateway.OverlayRequest
  alias Platform.Nats.Planes

  @snapshot "rpc.internal.overlays.feed.snapshot"
  @listed ["eventlist", "chatbox"]

  def events(%{"widgets" => widgets}) when is_list(widgets) do
    widgets
    |> Enum.filter(&match?(%{"kind" => kind} when kind in @listed, &1))
    |> Enum.map(&count/1)
    |> Enum.max(fn -> 0 end)
  end

  def events(_document), do: 0

  defp count(%{"settings" => %{"count" => count}}) when is_number(count), do: max(0, trunc(count))
  defp count(_widget), do: 0

  def snapshot(user_id, events, rpc) do
    requester = &Planes.request(&1, rpc)
    body = %{user_id: Integer.to_string(user_id), events: events}

    case OverlayRequest.request(requester, @snapshot, body) do
      {:ok, %{"snapshot" => %{} = snapshot}} -> {:ok, snapshot}
      other -> {:error, inspect(other, limit: 5)}
    end
  end
end
