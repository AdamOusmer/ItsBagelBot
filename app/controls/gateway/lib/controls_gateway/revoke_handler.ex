# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.RevokeHandler do
  @moduledoc "Authenticated native self-revocation without dashboard cookies."
  alias ControlsGateway.Config

  @headers %{
    "content-type" => "application/json",
    "cache-control" => "no-store",
    "pragma" => "no-cache"
  }

  def init(req, _opts) do
    cond do
      :cowboy_req.method(req) != "POST" ->
        {:ok, :cowboy_req.reply(405, Map.put(@headers, "allow", "POST"), "", req), nil}

      Config.auth_revoke_url() == "" ->
        reply(req, 503, %{"error" => "server_error"})

      true ->
        read_revoke(req)
    end
  end

  defp read_revoke(req) do
    case :cowboy_req.read_body(req, %{length: 1, period: 5_000}) do
      {:ok, "", next} -> revoke(next)
      {_, _, next} -> reply(next, 400, %{"error" => "invalid_request"})
    end
  end

  defp revoke(req) do
    with "DPoP " <> token when byte_size(token) > 0 and byte_size(token) <= 8192 <-
           :cowboy_req.header("authorization", req, ""),
         proof when byte_size(proof) > 0 and byte_size(proof) <= 8192 <-
           :cowboy_req.header("dpop", req, ""),
         {:ok, result} <-
           Config.auth_rpc().revoke(%{
             "token" => token,
             "proof" => proof,
             "method" => "POST",
             "url" => Config.auth_revoke_url()
           }) do
      respond(req, result)
    else
      {:error, _} -> reply(req, 503, %{"error" => "server_error"})
      _ -> reply(req, 401, %{"error" => "invalid_token"})
    end
  end

  defp respond(req, %{"ok" => true}), do: {:ok, :cowboy_req.reply(204, @headers, "", req), nil}

  defp respond(req, %{"oauth_error" => "use_dpop_nonce", "dpop_nonce" => nonce})
       when is_binary(nonce) and byte_size(nonce) <= 2048 do
    if not Regex.match?(~r/^[A-Za-z0-9_-]{1,512}$/, nonce) do
      reply(req, 503, %{"error" => "server_error"})
    else
      headers =
        @headers
        |> Map.put("dpop-nonce", nonce)
        |> Map.put("www-authenticate", ~s(DPoP error="use_dpop_nonce"))

      {:ok, :cowboy_req.reply(400, headers, Jason.encode!(%{"error" => "use_dpop_nonce"}), req),
       nil}
    end
  end

  defp respond(req, %{"oauth_error" => error}) when error in ~w(invalid_token invalid_dpop_proof),
    do: reply(req, 401, %{"error" => error})

  defp respond(req, _), do: reply(req, 503, %{"error" => "server_error"})

  defp reply(req, status, body),
    do: {:ok, :cowboy_req.reply(status, @headers, Jason.encode!(body), req), nil}
end
