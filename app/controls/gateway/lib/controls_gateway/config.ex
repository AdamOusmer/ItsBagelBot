# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Config do
  def auth_token_url, do: Application.get_env(:controls_gateway, :auth_token_url, "")

  def auth_revoke_url, do: Application.get_env(:controls_gateway, :auth_revoke_url, "")

  def auth_prefix,
    do: Application.get_env(:controls_gateway, :auth_prefix, "bagel.rpc.overlay.device")

  def auth_rpc, do: Application.get_env(:controls_gateway, :auth_rpc, ControlsGateway.AuthRpc)

  def nats, do: Application.fetch_env!(:controls_gateway, :nats)

  def port, do: Application.get_env(:controls_gateway, :port, 4000)

  def server?, do: Application.get_env(:controls_gateway, :server, true)

  def token_impl,
    do: Application.get_env(:controls_gateway, :token_impl, ControlsGateway.Token.Ed25519)

  def token_public_key(kid) do
    Application.get_env(:controls_gateway, :token_public_keys, %{}) |> Map.get(kid)
  end
end
