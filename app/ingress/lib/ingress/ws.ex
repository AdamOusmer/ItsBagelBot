defmodule Ingress.WS do
  @moduledoc """
  Minimal WebSocket connection handle built on `Mint.WebSocket`.

  Mint is process-less: the owning GenServer receives the raw transport
  messages and feeds them through `stream/2`. Nothing about the socket
  lifecycle is hidden from the owner, which is exactly what the v1 library
  could not give us (see ADR 0001/0006).

  `stream/2` returns `:unknown` when the message belongs to a different
  connection, so one process can hold two handles at once during the EventSub
  reconnect handshake.
  """

  require Logger

  defstruct [:conn, :ref, :websocket, :status, :headers]

  @type t :: %__MODULE__{}
  @type event ::
          :upgraded
          | {:frame, Mint.WebSocket.frame()}
          | {:closed, term()}

  @spec connect(String.t()) :: {:ok, t()} | {:error, term()}
  def connect(url) do
    uri = URI.parse(url)
    {scheme, ws_scheme, default_port} = schemes(uri.scheme)
    port = uri.port || default_port
    path = (uri.path || "/") <> if(uri.query, do: "?" <> uri.query, else: "")

    with {:ok, conn} <-
           Mint.HTTP.connect(scheme, uri.host, port, protocols: [:http1], transport_opts: []),
         {:ok, conn, ref} <- Mint.WebSocket.upgrade(ws_scheme, conn, path, []) do
      {:ok, %__MODULE__{conn: conn, ref: ref}}
    else
      {:error, reason} -> {:error, reason}
      {:error, _conn, reason} -> {:error, reason}
    end
  end

  defp schemes("wss"), do: {:https, :wss, 443}
  defp schemes("https"), do: {:https, :wss, 443}
  defp schemes(_), do: {:http, :ws, 80}

  @doc """
  Feeds a raw VM message into the connection.

  Returns `{:ok, ws, events}`, `{:error, ws, reason, events}` when the
  transport failed (events seen before the failure are still delivered), or
  `:unknown` when the message is not for this connection.
  """
  @spec stream(t() | nil, term()) ::
          {:ok, t(), [event()]} | {:error, t(), term(), [event()]} | :unknown
  def stream(nil, _message), do: :unknown

  def stream(%__MODULE__{} = ws, message) do
    case Mint.WebSocket.stream(ws.conn, message) do
      :unknown ->
        :unknown

      {:ok, conn, responses} ->
        collect(%{ws | conn: conn}, responses, [])

      {:error, conn, reason, responses} ->
        case collect(%{ws | conn: conn}, responses, []) do
          {:ok, ws, events} -> {:error, ws, reason, events}
          {:error, ws, first_reason, events} -> {:error, ws, first_reason, events}
        end
    end
  end

  defp collect(ws, [], events), do: {:ok, ws, Enum.reverse(events)}

  defp collect(%{ref: ref} = ws, [{:status, ref, status} | rest], events),
    do: collect(%{ws | status: status}, rest, events)

  defp collect(%{ref: ref} = ws, [{:headers, ref, headers} | rest], events) do
    case Mint.WebSocket.new(ws.conn, ref, ws.status, headers) do
      {:ok, conn, websocket} ->
        collect(%{ws | conn: conn, websocket: websocket, headers: headers}, rest, [
          :upgraded | events
        ])

      {:error, conn, reason} ->
        {:error, %{ws | conn: conn}, {:upgrade_failed, reason}, Enum.reverse(events)}
    end
  end

  defp collect(%{ref: ref} = ws, [{:data, ref, data} | rest], events) do
    case Mint.WebSocket.decode(ws.websocket, data) do
      {:ok, websocket, frames} ->
        frames = Enum.map(frames, &{:frame, &1})
        collect(%{ws | websocket: websocket}, rest, Enum.reverse(frames) ++ events)

      {:error, websocket, reason} ->
        {:error, %{ws | websocket: websocket}, {:decode_failed, reason}, Enum.reverse(events)}
    end
  end

  defp collect(%{ref: ref} = ws, [{:done, ref} | rest], events), do: collect(ws, rest, events)

  defp collect(%{ref: ref} = ws, [{:error, ref, reason} | rest], events),
    do: collect(ws, rest, [{:closed, reason} | events])

  defp collect(ws, [_other | rest], events), do: collect(ws, rest, events)

  @spec send_frame(t(), Mint.WebSocket.frame() | Mint.WebSocket.shorthand_frame()) ::
          {:ok, t()} | {:error, term()}
  def send_frame(%__MODULE__{websocket: nil}, _frame), do: {:error, :not_upgraded}

  def send_frame(%__MODULE__{} = ws, frame) do
    with {:ok, websocket, data} <- Mint.WebSocket.encode(ws.websocket, frame),
         {:ok, conn} <- Mint.WebSocket.stream_request_body(ws.conn, ws.ref, data) do
      {:ok, %{ws | conn: conn, websocket: websocket}}
    else
      {:error, _state, reason} -> {:error, reason}
      {:error, reason} -> {:error, reason}
    end
  end

  @doc "Best-effort polite close: send a close frame, then close the transport."
  @spec close(t() | nil) :: :ok
  def close(nil), do: :ok

  def close(%__MODULE__{} = ws) do
    ws =
      case send_frame(ws, {:close, 1000, ""}) do
        {:ok, ws} -> ws
        {:error, _} -> ws
      end

    Mint.HTTP.close(ws.conn)
    :ok
  end
end
