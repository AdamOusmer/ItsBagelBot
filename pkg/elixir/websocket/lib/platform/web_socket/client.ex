# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.WebSocket.Client do
  alias Platform.WebSocket.Frame

  defstruct [:conn, :ref, :websocket, :status, :headers, frames: %Frame{}]

  @opaque t :: %__MODULE__{}
  @type frame ::
          {:text, String.t()}
          | {:binary, binary()}
          | {:ping, binary()}
          | {:pong, binary()}
          | {:close, non_neg_integer() | nil, binary() | nil}
  @type control_frame :: :ping | :pong | :close
  @type event ::
          :upgraded
          | {:frame, frame()}
          | {:closed, term()}

  @spec connect(String.t(), keyword()) :: {:ok, t()} | {:error, term()}
  def connect(url, opts \\ []) do
    uri = URI.parse(url)

    with {:ok, scheme, ws_scheme, default_port} <- schemes(uri.scheme),
         true <- is_binary(uri.host) and uri.host != "" do
      port = uri.port || default_port
      path = (uri.path || "/") <> if(uri.query, do: "?" <> uri.query, else: "")
      transport_opts = Keyword.get(opts, :transport_opts, [])
      headers = Keyword.get(opts, :headers, [])

      with {:ok, conn} <-
             Mint.HTTP.connect(scheme, uri.host, port,
               protocols: [:http1],
               transport_opts: transport_opts
             ),
           {:ok, conn, ref} <- Mint.WebSocket.upgrade(ws_scheme, conn, path, headers) do
        {:ok, %__MODULE__{conn: conn, ref: ref}}
      else
        {:error, reason} ->
          {:error, reason(reason)}

        {:error, conn, reason} ->
          Mint.HTTP.close(conn)
          {:error, reason(reason)}
      end
    else
      _ -> {:error, :invalid_url}
    end
  end

  defp schemes("wss"), do: {:ok, :https, :wss, 443}
  defp schemes("https"), do: {:ok, :https, :wss, 443}
  defp schemes("ws"), do: {:ok, :http, :ws, 80}
  defp schemes("http"), do: {:ok, :http, :ws, 80}
  defp schemes(_), do: {:error, :invalid_url}

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
          {:ok, ws, events} -> {:error, ws, reason(reason), events}
          {:error, ws, first_reason, events} -> {:error, ws, first_reason, events}
        end
    end
  end

  defp collect(ws, [], events), do: {:ok, ws, Enum.reverse(events)}

  defp collect(%{ref: ref} = ws, [{:status, ref, status} | rest], events),
    do: collect(%{ws | status: status}, rest, events)

  defp collect(%{ref: ref, status: 101} = ws, [{:headers, ref, headers} | rest], events) do
    case Mint.WebSocket.new(ws.conn, ref, ws.status, headers) do
      {:ok, conn, websocket} ->
        collect(%{ws | conn: conn, websocket: websocket, headers: headers}, rest, [
          :upgraded | events
        ])

      {:error, conn, reason} ->
        {:error, %{ws | conn: conn}, {:upgrade_failed, reason(reason)}, Enum.reverse(events)}
    end
  end

  defp collect(%{ref: ref, websocket: nil} = ws, [{:headers, ref, headers} | _], events),
    do: {:error, %{ws | headers: headers}, {:http_status, ws.status}, Enum.reverse(events)}

  defp collect(%{ref: ref} = ws, [{:data, ref, data} | rest], events) do
    case Frame.decode(ws.frames, data) do
      {:ok, state, frames} ->
        collect(%{ws | frames: state}, rest, Enum.reduce(frames, events, &[{:frame, &1} | &2]))

      {:error, reason} ->
        {:error, ws, {:decode_failed, reason}, Enum.reverse(events)}
    end
  end

  defp collect(%{ref: ref} = ws, [{:done, ref} | rest], events), do: collect(ws, rest, events)

  defp collect(%{ref: ref} = ws, [{:error, ref, reason} | rest], events),
    do: collect(ws, rest, [{:closed, reason(reason)} | events])

  defp collect(ws, [_other | rest], events), do: collect(ws, rest, events)

  @spec send_frame(t(), frame() | control_frame()) ::
          {:ok, t()} | {:error, term()}
  def send_frame(%__MODULE__{websocket: nil}, _frame), do: {:error, :not_upgraded}

  def send_frame(%__MODULE__{} = ws, frame) do
    with {:ok, websocket, data} <- Mint.WebSocket.encode(ws.websocket, frame),
         {:ok, conn} <- Mint.WebSocket.stream_request_body(ws.conn, ws.ref, data) do
      {:ok, %{ws | conn: conn, websocket: websocket}}
    else
      {:error, _state, reason} -> {:error, reason(reason)}
      {:error, reason} -> {:error, reason(reason)}
    end
  end

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

  defp reason(%{__exception__: true, reason: reason}), do: reason
  defp reason(reason), do: reason
end
