# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Config do
  @default_sandbox_origin "https://widgets.itsbagelbot.com"
  @default_overlay_origin "https://overlay.itsbagelbot.com"

  def sandbox_origin,
    do: Application.get_env(:controls_gateway, :overlay_sandbox_origin, @default_sandbox_origin)

  def overlay_origin,
    do: Application.get_env(:controls_gateway, :overlay_origin, @default_overlay_origin)

  def nats, do: Application.fetch_env!(:controls_gateway, :nats)

  def port, do: Application.get_env(:controls_gateway, :port, 4000)

  def server?, do: Application.get_env(:controls_gateway, :server, true)

  def token_impl,
    do: Application.get_env(:controls_gateway, :token_impl, ControlsGateway.Token.Ed25519)

  def token_public_key(kid) do
    Application.get_env(:controls_gateway, :token_public_keys, %{}) |> Map.get(kid)
  end
end
