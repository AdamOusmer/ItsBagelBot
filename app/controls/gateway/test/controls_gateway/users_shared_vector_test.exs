# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.UsersSharedVectorTest do
  @moduledoc """
  Cross-service fixed vector from Users' tokensign package (also committed
  there as TestSignMatchesCommittedFixedVector / TestVerifyMatchesCommittedFixedVector).
  Throwaway key: seed is "test-device-auth-seed-0123456789", not used anywhere
  else. Regenerate both sides together if this ever changes.
  """
  # async: false: shares the :controls_gateway, :token_public_keys Application
  # env (global) with ControlsGateway.Token.Ed25519Test.
  use ExUnit.Case, async: false

  alias ControlsGateway.Token.Ed25519

  @kid "test-2026-11-06"
  @public_key Base.decode64!("+DN3zqgrYzQDwDXEQGVJEV2AvSe6oJRxaNYbgiYmD7E=")
  @seed Base.decode64!("dGVzdC1kZXZpY2UtYXV0aC1zZWVkLTAxMjM0NTY3ODk=")
  @token "v1.eyJraWQiOiJ0ZXN0LTIwMjYtMTEtMDYiLCJpc3MiOiJpdHNiYWdlbGJvdC11c2VycyIsImF1ZCI6ImNvbnRyb2xzLWdhdGV3YXkiLCJzdWIiOiI4MDQ5MzI5ODQiLCJkZXZpY2VfaWQiOiIxMTExMTExMS0xMTExLTQxMTEtODExMS0xMTExMTExMTExMTEiLCJncmFudF9pZCI6IjIyMjIyMjIyLTIyMjItNDIyMi04MjIyLTIyMjIyMjIyMjIyMiIsImlhdCI6MTgwMDAwMDAwMCwiZXhwIjo0MTAyNDQ0ODAwLCJqdGkiOiIzMzMzMzMzMy0zMzMzLTQzMzMtODMzMy0zMzMzMzMzMzMzMzMifQ.bG0TaPU8re3MxTynJD7O4rN4hWKQGTl2FOC364LRkr6XT-RkEvC7pTZZAfXR2jiniHC4WXTitTgIbgn8q4yoCg"

  setup do
    Application.put_env(:controls_gateway, :token_public_keys, %{@kid => @public_key})
    :ok
  end

  test "verifies the exact token Users produced, to the exact claims" do
    assert {:ok, claims} = Ed25519.verify(@token)

    assert claims == %{
             sub: "804932984",
             device_id: "11111111-1111-4111-8111-111111111111",
             grant_id: "22222222-2222-4222-8222-222222222222",
             jti: "33333333-3333-4333-8333-333333333333",
             iat: 1_800_000_000,
             exp: 4_102_444_800
           }
  end

  test "rejects the Users vector with a tampered signature" do
    [v, payload, sig] = String.split(@token, ".")
    bad_sig = flip_last_byte(sig)
    assert Ed25519.verify(Enum.join([v, payload, bad_sig], ".")) == {:error, :bad_signature}
  end

  test "rejects a token signed by the same key with the wrong audience" do
    {_pub, priv} = :crypto.generate_key(:eddsa, :ed25519, @seed)

    claims = %{
      "kid" => @kid,
      "iss" => "itsbagelbot-users",
      "aud" => "some-other-service",
      "sub" => "804932984",
      "device_id" => "11111111-1111-4111-8111-111111111111",
      "grant_id" => "22222222-2222-4222-8222-222222222222",
      "iat" => 1_800_000_000,
      "exp" => 4_102_444_800,
      "jti" => "33333333-3333-4333-8333-333333333333"
    }

    payload_b64 = claims |> Jason.encode!() |> Base.url_encode64(padding: false)
    message = "v1." <> payload_b64
    signature = :crypto.sign(:eddsa, :none, message, [priv, :ed25519])
    sig_b64 = Base.url_encode64(signature, padding: false)
    token = "v1." <> payload_b64 <> "." <> sig_b64

    assert Ed25519.verify(token) == {:error, :wrong_audience}
  end

  defp flip_last_byte(b64) do
    bin = Base.url_decode64!(b64, padding: false)
    size = byte_size(bin) - 1
    <<head::binary-size(^size), last>> = bin
    Base.url_encode64(<<head::binary, Bitwise.bxor(last, 1)>>, padding: false)
  end
end
