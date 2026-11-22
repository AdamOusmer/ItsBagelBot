# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.OverlayRequest do
  @moduledoc "Client address, link token and JSON request helpers shared by the overlay routes."

  alias ControlsGateway.SignedCookie

  @max_body 256

  def client_ip(req) do
    {peer, _port} = :cowboy_req.peer(req)

    if Application.get_env(:controls_gateway, :overlay_trust_cloudflare_ip, false),
      do: forwarded_ip(req, peer),
      else: peer
  end

  defp forwarded_ip(req, peer) do
    address = :cowboy_req.header("cf-connecting-ip", req, "")

    case :inet.parse_strict_address(:binary.bin_to_list(address)) do
      {:ok, ip} -> ip
      {:error, :einval} -> peer
    end
  end

  def token_hash(req) do
    with key when is_binary(key) and byte_size(key) >= 32 <- SignedCookie.key(),
         {"application", "json", _params} <- :cowboy_req.parse_header("content-type", req) do
      read_hash(req)
    else
      key when is_nil(key) or is_binary(key) -> {{:unavailable, :cookie_key}, req}
      _ -> {{:not_found, :content_type}, req}
    end
  catch
    :exit, {:request_error, _, _} -> {{:not_found, :request_error}, req}
  end

  defp read_hash(req) do
    case :cowboy_req.read_body(req, %{length: @max_body, period: 2_000}) do
      {:ok, body, req} when byte_size(body) <= @max_body -> {hash(body), req}
      {_, _, req} -> {{:not_found, :body_size}, req}
    end
  end

  defp hash(body) do
    case token(body) do
      {:ok, token} -> {:ok, :crypto.hash(:sha256, token) |> Base.encode16(case: :lower)}
      :error -> {:not_found, :token_format}
    end
  end

  defp token(body) do
    with {:ok, %{"token" => token}} when is_binary(token) and byte_size(token) == 43 <-
           Jason.decode(body),
         {:ok, raw} when byte_size(raw) == 32 <- Base.url_decode64(token, padding: false),
         ^token <- Base.url_encode64(raw, padding: false) do
      {:ok, token}
    else
      _ -> :error
    end
  end

  def request(requester, subject, body) do
    with {:ok, %{body: json}} <-
           requester.({subject, Jason.encode!(body), [receive_timeout: 2_000]}) do
      Jason.decode(json)
    end
  end
end
