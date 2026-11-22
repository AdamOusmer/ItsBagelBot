# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.OverlayRequest do
  @moduledoc "Client address, link token and leaf RPC helpers shared by the overlay routes."

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

  def token(body) do
    with {:ok, %{"token" => token}} when is_binary(token) and byte_size(token) == 43 <-
           Jason.decode(body),
         {:ok, raw} when byte_size(raw) == 32 <- Base.url_decode64(token, padding: false),
         ^token <- Base.url_encode64(raw, padding: false) do
      {:ok, token}
    else
      _ -> :error
    end
  end

  def request(nil, _subject, _body), do: {:error, :not_connected}

  def request(conn, subject, body) do
    with {:ok, %{body: json}} <-
           Gnat.request(conn, subject, Jason.encode!(body), receive_timeout: 2_000) do
      Jason.decode(json)
    end
  catch
    :exit, _ -> {:error, :not_connected}
  end
end
