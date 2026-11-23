# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Feed do
  alias ControlsGateway.OverlayRequest
  alias Platform.Nats.Planes

  @snapshot "rpc.internal.overlays.feed.snapshot"

  def snapshot(user_id, rpc) do
    requester = &Planes.request(&1, rpc)
    body = %{user_id: Integer.to_string(user_id)}

    case OverlayRequest.request(requester, @snapshot, body) do
      {:ok, %{"snapshot" => %{} = snapshot}} -> {:ok, snapshot}
      other -> {:error, inspect(other, limit: 5)}
    end
  end
end
