# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Nats do
  alias ControlsGateway.OverlayRequest
  alias Platform.Nats.Planes

  @resolve "rpc.internal.projector.overlays.powered.resolve"

  def resolve(token) do
    case OverlayRequest.request(&Planes.request/1, @resolve, %{token: token}) do
      {:ok, %{"code" => "not_found"}} -> :not_found
      {:ok, %{"code" => code}} when code not in [nil, ""] -> :unavailable
      {:ok, %{"config" => config, "premium" => premium}} -> overlay(config, premium)
      _ -> :unavailable
    end
  end

  defp overlay(config, premium) when is_map(config) and is_boolean(premium),
    do: {:ok, %{config: config, premium: premium}}

  defp overlay(_config, _premium), do: :unavailable
end
