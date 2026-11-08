# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Auth do
  @moduledoc "Native OAuth adapter. No browser session or dashboard dependency."
  alias ControlsGateway.Config
  @client "itsbagelbot-desktop"
  @allowed ~w(client_id grant_type code code_verifier redirect_uri refresh_token)
  @errors ~w(invalid_client invalid_request invalid_grant unsupported_grant_type invalid_dpop_proof use_dpop_nonce)

  def exchange(body, proof) when is_binary(body) and is_binary(proof) do
    with :ok <- configured(),
         :ok <- proof_present(proof),
         {:ok, form} <- parse_form(body),
         :ok <- validate_form(form),
         {:ok, reply} <-
           Config.auth_rpc().token(
             Map.merge(form, %{"proof" => proof, "url" => Config.auth_token_url()})
           ) do
      response(reply)
    else
      {:error, :proof} -> {400, %{"error" => "invalid_dpop_proof"}, nil}
      {:error, :request} -> {400, %{"error" => "invalid_request"}, nil}
      _ -> {503, %{"error" => "server_error"}, nil}
    end
  end

  defp configured do
    if Config.auth_token_url() == "", do: {:error, :unavailable}, else: :ok
  end

  defp proof_present(proof) do
    if byte_size(proof) > 0 and byte_size(proof) <= 8192, do: :ok, else: {:error, :proof}
  end

  def authorize(token, proof, method, public_url) do
    Config.auth_rpc().authorize(%{
      "token" => token,
      "proof" => proof,
      "method" => method,
      "url" => public_url
    })
  end

  defp parse_form(body) do
    body |> URI.query_decoder() |> Enum.reduce_while({:ok, %{}}, &form_field/2)
  rescue
    _ -> {:error, :request}
  end

  defp form_field({key, value}, {:ok, form}) do
    if key in @allowed and not Map.has_key?(form, key) do
      {:cont, {:ok, Map.put(form, key, value)}}
    else
      {:halt, {:error, :request}}
    end
  end

  defp validate_form(%{"client_id" => @client, "grant_type" => "authorization_code"} = form) do
    if bounded?(form["code"], 512) and bounded?(form["code_verifier"], 128) and
         bounded?(form["redirect_uri"], 255), do: :ok, else: {:error, :request}
  end

  defp validate_form(%{"client_id" => @client, "grant_type" => "refresh_token"} = form) do
    if bounded?(form["refresh_token"], 516), do: :ok, else: {:error, :request}
  end

  defp validate_form(_), do: {:error, :request}

  defp bounded?(value, max) when is_binary(value),
    do: byte_size(value) > 0 and byte_size(value) <= max

  defp bounded?(_, _), do: false

  defp response(
         %{
           "access_token" => access,
           "token_type" => "DPoP",
           "refresh_token" => refresh,
           "expires_in" => expiry
         } = reply
       )
       when is_binary(access) and is_binary(refresh) and is_integer(expiry) and expiry > 0 do
    {200,
     Map.take(reply, ~w(access_token token_type refresh_token expires_in grant_id device_id)),
     nil}
  end

  defp response(%{"oauth_error" => error} = reply) when error in @errors do
    nonce = reply["dpop_nonce"]

    if error == "use_dpop_nonce" and not valid_nonce?(nonce),
      do: {503, %{"error" => "server_error"}, nil},
      else: {400, %{"error" => error}, nonce}
  end

  defp response(_), do: {503, %{"error" => "server_error"}, nil}

  defp valid_nonce?(value) when is_binary(value),
    do: byte_size(value) <= 512 and Regex.match?(~r/^[A-Za-z0-9_-]+$/, value)

  defp valid_nonce?(_), do: false
end
