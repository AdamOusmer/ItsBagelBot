# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.OverlayRequest do
  @moduledoc "Link token and JSON request helpers shared by the overlay routes."

  alias ControlsGateway.SignedCookie

  @max_body 256

  def json?(conn) do
    with [type | _] <- Plug.Conn.get_req_header(conn, "content-type"),
         {:ok, "application", "json", _params} <- Plug.Conn.Utils.media_type(type) do
      true
    else
      _ -> false
    end
  end

  def read_body(conn, max) do
    case Plug.Conn.read_body(conn, length: max, read_length: max, read_timeout: 2_000) do
      {:ok, body, conn} when byte_size(body) <= max -> {{:ok, body}, conn}
      {:ok, _body, conn} -> {{:error, :body_size}, conn}
      {:more, _body, conn} -> {{:error, :body_size}, conn}
      {:error, _reason} -> {{:error, :request_error}, conn}
    end
  end

  def token_hash(conn) do
    with key when is_binary(key) and byte_size(key) >= 32 <- SignedCookie.key(),
         true <- json?(conn) do
      read_hash(conn)
    else
      key when is_nil(key) or is_binary(key) -> {{:unavailable, :cookie_key}, conn}
      _ -> {{:not_found, :content_type}, conn}
    end
  end

  defp read_hash(conn) do
    case read_body(conn, @max_body) do
      {{:ok, body}, conn} -> {hash(body), conn}
      {{:error, reason}, conn} -> {{:not_found, reason}, conn}
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
