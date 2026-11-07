# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.WsClient do
  @moduledoc """
  Minimal blocking WebSocket client over Mint.WebSocket, for exercising the
  gateway's real socket behavior from tests. Test-only: never shipped.
  """
  defstruct [:conn, :ref, :websocket, :status, :headers]

  @spec connect(:inet.port_number(), [{String.t(), String.t()}], String.t()) ::
          {:ok, struct()} | {:error, term()}
  def connect(port, headers \\ [], path \\ "/v1/connect") do
    with {:ok, conn} <- Mint.HTTP.connect(:http, "127.0.0.1", port, protocols: [:http1]),
         {:ok, conn, ref} <- Mint.WebSocket.upgrade(:ws, conn, path, headers) do
      await_upgrade(%__MODULE__{conn: conn, ref: ref})
    end
  end

  defp await_upgrade(client) do
    receive do
      message -> stream_upgrade(client, message)
    after
      2_000 -> {:error, :timeout}
    end
  end

  defp stream_upgrade(client, message) do
    case Mint.WebSocket.stream(client.conn, message) do
      {:ok, conn, responses} -> process_upgrade(%{client | conn: conn}, responses)
      {:error, _conn, reason, _responses} -> {:error, reason}
      # A stray message for a sibling client sharing this test process' mailbox.
      :unknown -> await_upgrade(client)
    end
  end

  defp process_upgrade(client, [{:status, ref, status} | rest]) when ref == client.ref do
    process_upgrade(%{client | status: status}, rest)
  end

  defp process_upgrade(client, [{:headers, ref, headers} | rest]) when ref == client.ref do
    case client.status do
      101 -> upgrade_websocket(%{client | headers: headers}, rest)
      _other -> process_upgrade(%{client | headers: headers}, rest)
    end
  end

  defp process_upgrade(client, [{:data, ref, _data} | rest]) when ref == client.ref do
    process_upgrade(client, rest)
  end

  defp process_upgrade(client, [{:done, ref} | _rest]) when ref == client.ref do
    {:error, {:http_status, client.status}}
  end

  defp process_upgrade(client, [_other | rest]), do: process_upgrade(client, rest)
  defp process_upgrade(client, []), do: await_upgrade(client)

  defp upgrade_websocket(client, _rest) do
    case Mint.WebSocket.new(client.conn, client.ref, client.status, client.headers) do
      {:ok, conn, websocket} -> {:ok, %{client | conn: conn, websocket: websocket}}
      {:error, _conn, reason} -> {:error, reason}
    end
  end

  @spec send_text(struct(), String.t()) :: struct()
  def send_text(client, text) do
    {:ok, websocket, data} = Mint.WebSocket.encode(client.websocket, {:text, text})
    {:ok, conn} = Mint.WebSocket.stream_request_body(client.conn, client.ref, data)
    %{client | conn: conn, websocket: websocket}
  end

  @spec recv_frames(struct(), non_neg_integer()) ::
          {:ok, [term()], struct()} | {:error, term(), struct()}
  def recv_frames(client, timeout \\ 2_000) do
    receive do
      message -> handle_recv(client, message, timeout)
    after
      timeout -> {:error, :timeout, client}
    end
  end

  defp handle_recv(client, message, timeout) do
    case Mint.WebSocket.stream(client.conn, message) do
      {:ok, conn, responses} ->
        {frames, client} = extract_frames(%{client | conn: conn}, responses)
        if frames == [], do: recv_frames(client, timeout), else: {:ok, frames, client}

      {:error, conn, reason, _responses} ->
        {:error, reason, %{client | conn: conn}}

      # A stray message for a sibling client sharing this test process' mailbox.
      :unknown ->
        recv_frames(client, timeout)
    end
  end

  defp extract_frames(client, responses) do
    Enum.reduce(responses, {[], client}, fn
      {:data, ref, data}, {frames, client} when ref == client.ref ->
        {:ok, websocket, decoded} = Mint.WebSocket.decode(client.websocket, data)
        {frames ++ decoded, %{client | websocket: websocket}}

      _other, acc ->
        acc
    end)
  end

  @spec close(struct()) :: :ok
  def close(client) do
    case client.websocket && Mint.WebSocket.encode(client.websocket, {:close, 1000, ""}) do
      {:ok, _websocket, data} -> Mint.WebSocket.stream_request_body(client.conn, client.ref, data)
      _ -> :ok
    end

    Mint.HTTP.close(client.conn)
    :ok
  end
end
