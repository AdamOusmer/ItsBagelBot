# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Token.Ed25519Test do
  # async: false: shares the :controls_gateway, :token_public_keys Application
  # env (global) with ControlsGateway.UsersSharedVectorTest.
  use ExUnit.Case, async: false

  alias ControlsGateway.TestSupport.TokenFixture
  alias ControlsGateway.Token.Ed25519

  # Fixed vector: seed <<42, 42, ..., 42>> (32 bytes), so Users can sign the
  # identical token for a shared cross-service test. Regenerate both sides
  # together if this ever changes.
  @fixed_public_key Base.decode64!("GX9rI+FshTLGq8g4+s1ep4m+DHaykgM0A5v6iz02jWE=")
  @fixed_token "v1.eyJhdWQiOiJjb250cm9scy1nYXRld2F5IiwiZGV2aWNlX2lkIjoiMTExMTExMTEtMTExMS00MTExLTgxMTEtMTExMTExMTExMTExIiwiZXhwIjo0ODAwMDAwMDAwLCJncmFudF9pZCI6IjIyMjIyMjIyLTIyMjItNDIyMi04MjIyLTIyMjIyMjIyMjIyMiIsImlhdCI6MTgwMDAwMDAwMCwiaXNzIjoiaXRzYmFnZWxib3QtdXNlcnMiLCJqdGkiOiIzMzMzMzMzMy0zMzMzLTQzMzMtODMzMy0zMzMzMzMzMzMzMzMiLCJraWQiOiJ0ZXN0LTEiLCJzdWIiOiJ1c2VyLTAwMDEifQ.UMdKM3NAzNDZSNVUpAs34VY_HXDCjpdDp1cxg1QDYTVziQbiyo6KOoG7wK1ZIrk6B7R_lcm_xnC3lw5MH4GrCQ"

  setup do
    Application.put_env(:controls_gateway, :token_public_keys, %{"test-1" => @fixed_public_key})
    :ok
  end

  test "known-good vector verifies to the exact claims (shared with Users)" do
    assert {:ok, claims} = Ed25519.verify(@fixed_token)

    assert claims == %{
             sub: "user-0001",
             device_id: "11111111-1111-4111-8111-111111111111",
             grant_id: "22222222-2222-4222-8222-222222222222",
             jti: "33333333-3333-4333-8333-333333333333",
             iat: 1_800_000_000,
             exp: 4_800_000_000
           }
  end

  test "rejects a flipped signature byte" do
    [v, payload, sig] = String.split(@fixed_token, ".")
    bad_sig = flip_last_byte(sig)
    assert Ed25519.verify(Enum.join([v, payload, bad_sig], ".")) == {:error, :bad_signature}
  end

  test "rejects an unknown kid" do
    Application.put_env(:controls_gateway, :token_public_keys, %{})
    assert Ed25519.verify(@fixed_token) == {:error, :unknown_kid}
  end

  test "rejects an expired token" do
    {public_key, private_key} = TokenFixture.keypair()
    Application.put_env(:controls_gateway, :token_public_keys, %{"test" => public_key})

    claims = TokenFixture.claims(%{"exp" => System.system_time(:second) - 10})
    token = TokenFixture.sign(private_key, claims)

    assert Ed25519.verify(token) == {:error, :expired}
  end

  test "rejects the wrong audience" do
    {public_key, private_key} = TokenFixture.keypair()
    Application.put_env(:controls_gateway, :token_public_keys, %{"test" => public_key})

    claims = TokenFixture.claims(%{"aud" => "some-other-service"})
    token = TokenFixture.sign(private_key, claims)

    assert Ed25519.verify(token) == {:error, :wrong_audience}
  end

  test "rejects the wrong issuer" do
    {public_key, private_key} = TokenFixture.keypair()
    Application.put_env(:controls_gateway, :token_public_keys, %{"test" => public_key})

    claims = TokenFixture.claims(%{"iss" => "someone-else"})
    token = TokenFixture.sign(private_key, claims)

    assert Ed25519.verify(token) == {:error, :wrong_issuer}
  end

  test "ignores unknown fields in the payload" do
    {public_key, private_key} = TokenFixture.keypair()
    Application.put_env(:controls_gateway, :token_public_keys, %{"test" => public_key})

    claims = TokenFixture.claims(%{"extra_field" => "ignored"})
    token = TokenFixture.sign(private_key, claims)

    assert {:ok, _claims} = Ed25519.verify(token)
  end

  test "rejects malformed tokens" do
    assert Ed25519.verify("not-a-token") == {:error, :malformed}
    assert Ed25519.verify("v2.a.b") == {:error, :malformed}
    assert Ed25519.verify(123) == {:error, :malformed}
  end

  defp flip_last_byte(b64) do
    bin = Base.url_decode64!(b64, padding: false)
    size = byte_size(bin) - 1
    <<head::binary-size(^size), last>> = bin
    Base.url_encode64(<<head::binary, Bitwise.bxor(last, 1)>>, padding: false)
  end
end
