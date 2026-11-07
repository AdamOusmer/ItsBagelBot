# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Token.Ed25519 do
  @moduledoc """
  Verifies `v1.<base64url(payload)>.<base64url(signature)>` access tokens.
  Payload is JSON; the signed message is the ASCII string `v1.<base64url(payload)>`.
  Signing, rotation and issuance belong to Users; this module only checks.
  """
  @behaviour ControlsGateway.Token

  alias ControlsGateway.Config

  @version "v1"
  @issuer "itsbagelbot-users"
  @audience "controls-gateway"

  @impl true
  def verify(token) when is_binary(token) do
    with {:ok, payload_b64, signature} <- split_token(token),
         {:ok, payload_bin} <- decode_segment(payload_b64),
         {:ok, signature_bin} <- decode_segment(signature),
         {:ok, claims} <- Jason.decode(payload_bin),
         {:ok, public_key} <- select_key(claims),
         :ok <- check_signature(payload_b64, signature_bin, public_key),
         {:ok, parsed} <- parse_claims(claims) do
      check_bounds(parsed)
    end
  end

  def verify(_), do: {:error, :malformed}

  defp split_token(token) do
    case String.split(token, ".") do
      [@version, payload, signature] -> {:ok, payload, signature}
      _ -> {:error, :malformed}
    end
  end

  defp decode_segment(segment) do
    case Base.url_decode64(segment, padding: false) do
      {:ok, bin} -> {:ok, bin}
      :error -> {:error, :malformed}
    end
  end

  defp select_key(%{"kid" => kid}) when is_binary(kid) do
    case Config.token_public_key(kid) do
      nil -> {:error, :unknown_kid}
      key -> {:ok, key}
    end
  end

  defp select_key(_claims), do: {:error, :malformed}

  defp check_signature(payload_b64, signature, public_key) do
    message = @version <> "." <> payload_b64

    if byte_size(signature) == 64 and
         :crypto.verify(:eddsa, :none, message, signature, [public_key, :ed25519]) do
      :ok
    else
      {:error, :bad_signature}
    end
  end

  defp parse_claims(claims) do
    with {:ok, iss} <- fetch_string(claims, "iss"),
         {:ok, aud} <- fetch_string(claims, "aud"),
         {:ok, sub} <- fetch_string(claims, "sub"),
         {:ok, device_id} <- fetch_string(claims, "device_id"),
         {:ok, grant_id} <- fetch_string(claims, "grant_id"),
         {:ok, jti} <- fetch_string(claims, "jti"),
         {:ok, iat} <- fetch_integer(claims, "iat"),
         {:ok, exp} <- fetch_integer(claims, "exp") do
      {:ok,
       %{
         iss: iss,
         aud: aud,
         sub: sub,
         device_id: device_id,
         grant_id: grant_id,
         jti: jti,
         iat: iat,
         exp: exp
       }}
    end
  end

  defp check_bounds(%{iss: @issuer, aud: @audience} = claims) do
    if claims.exp > System.system_time(:second) do
      {:ok, Map.drop(claims, [:iss, :aud])}
    else
      {:error, :expired}
    end
  end

  defp check_bounds(%{iss: @issuer}), do: {:error, :wrong_audience}
  defp check_bounds(_claims), do: {:error, :wrong_issuer}

  defp fetch_string(claims, key) do
    case Map.get(claims, key) do
      value when is_binary(value) and value != "" -> {:ok, value}
      _ -> {:error, :missing_claim}
    end
  end

  defp fetch_integer(claims, key) do
    case Map.get(claims, key) do
      value when is_integer(value) -> {:ok, value}
      _ -> {:error, :missing_claim}
    end
  end
end
