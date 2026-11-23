# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Clips do
  require Logger

  alias ControlsGateway.{Drain, Http, Metrics}
  alias Platform.Nats.ObjectStore

  @bucket "OVERLAYS_MEDIA"
  @chunk_timeout_ms 2_000

  def reply(conn, session, headers) do
    Drain.track_connection(:download)
    event_id = conn.path_params["event_id"]
    key = "#{session.user_id}/#{event_id}"

    with true <- valid_event_id?(event_id),
         {:ok, info} <- ObjectStore.info(:jetstream, @bucket, key, timeout: @chunk_timeout_ms) do
      stream(conn, session, headers, info)
    else
      _ -> unavailable(conn, session, headers)
    end
  end

  defp valid_event_id?(event_id),
    do: String.valid?(event_id) and :binary.match(event_id, ["/", <<0>>]) == :nomatch

  defp clip_headers(info) do
    %{
      "content-type" => "audio/wav",
      "content-length" => Integer.to_string(info.size),
      "x-content-type-options" => "nosniff"
    }
  end

  defp stream(conn, session, headers, info) do
    idle = {:idle, conn, Map.merge(headers, clip_headers(info))}

    case ObjectStore.stream(info, idle, &relay/2) do
      {:ok, {:open, conn}} ->
        Metrics.count("Clip/Served")
        conn

      {:error, _reason, {:idle, conn, _headers}} ->
        unavailable(conn, session, headers)

      {:error, _reason, {:open, _conn}} ->
        interrupted()
    end
  end

  defp relay(data, {:idle, conn, headers}) do
    conn
    |> Plug.Conn.delete_resp_header("cache-control")
    |> Http.put_headers(headers)
    |> Plug.Conn.send_chunked(200)
    |> then(&relay(data, {:open, &1}))
  end

  defp relay(data, {:open, conn}) do
    case Plug.Conn.chunk(conn, data) do
      {:ok, conn} -> {:ok, {:open, conn}}
      {:error, reason} -> {:error, reason, {:open, conn}}
    end
  end

  defp interrupted do
    Metrics.count("Clip/Interrupted")
    Logger.warning("TTS clip read interrupted")
    exit(:shutdown)
  end

  defp unavailable(conn, session, headers) do
    Logger.warning("TTS clip unavailable", user_id: session.user_id)
    Http.reply(conn, 404, headers)
  end
end
