# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TokenHandler do
  @moduledoc "Bounded HTTP transport for native OAuth; session/proof policy belongs to Users."
  alias ControlsGateway.Auth

  @headers %{
    "content-type" => "application/json",
    "cache-control" => "no-store",
    "pragma" => "no-cache"
  }

  def init(req, _opts) do
    if :cowboy_req.method(req) == "POST" do
      handle_post(req)
    else
      {:ok, :cowboy_req.reply(405, Map.put(@headers, "allow", "POST"), "", req), nil}
    end
  end

  defp handle_post(req) do
    type =
      :cowboy_req.header("content-type", req, "") |> String.split(";") |> hd() |> String.trim()

    if type == "application/x-www-form-urlencoded",
      do: read_post(req),
      else: reply(req, {400, %{"error" => "invalid_request"}, nil})
  end

  defp read_post(req) do
    case :cowboy_req.read_body(req, %{length: 4096, period: 5_000}) do
      {:ok, body, next} when byte_size(body) <= 4096 ->
        proof = :cowboy_req.header("dpop", next, "")
        reply(next, Auth.exchange(body, proof))

      {_status, _body, next} ->
        reply(next, {400, %{"error" => "invalid_request"}, nil})
    end
  end

  defp reply(req, {status, body, nonce}) do
    headers =
      if is_binary(nonce) and byte_size(nonce) <= 2048,
        do:
          @headers
          |> Map.put("dpop-nonce", nonce)
          |> Map.put("www-authenticate", ~s(DPoP error="use_dpop_nonce")),
        else: @headers

    {:ok, :cowboy_req.reply(status, headers, Jason.encode!(body), req), nil}
  end
end
