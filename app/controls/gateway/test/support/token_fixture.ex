# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.TokenFixture do
  @moduledoc "Signs test access tokens matching the production wire format exactly."

  @version "v1"

  @spec keypair() :: {binary(), binary()}
  def keypair, do: :crypto.generate_key(:eddsa, :ed25519)

  @spec sign(binary(), map()) :: String.t()
  def sign(private_key, claims) do
    payload_b64 = claims |> Jason.encode!() |> Base.url_encode64(padding: false)
    message = @version <> "." <> payload_b64
    signature = :crypto.sign(:eddsa, :none, message, [private_key, :ed25519])
    sig_b64 = Base.url_encode64(signature, padding: false)
    @version <> "." <> payload_b64 <> "." <> sig_b64
  end

  @spec claims(map()) :: map()
  def claims(overrides \\ %{}) do
    now = System.system_time(:second)

    Map.merge(
      %{
        "kid" => "test",
        "iss" => "itsbagelbot-users",
        "aud" => "controls-gateway",
        "sub" => "user-1",
        "device_id" => "11111111-1111-4111-8111-111111111111",
        "grant_id" => "22222222-2222-4222-8222-222222222222",
        "iat" => now,
        "exp" => now + 900,
        "jti" => "33333333-3333-4333-8333-333333333333"
      },
      overrides
    )
  end
end
