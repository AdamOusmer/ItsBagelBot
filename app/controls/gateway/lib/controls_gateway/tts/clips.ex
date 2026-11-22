# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Clips do
  require Logger

  alias ControlsGateway.Tts.Nats
  alias Platform.Nats.Planes

  @bucket "TTS_CLIPS"
  @get "$JS.API.STREAM.MSG.GET.OBJ_TTS_CLIPS"

  def reply(req, session, headers) do
    event_id = :cowboy_req.binding(:event_id, req)

    with true <- valid_event_id?(event_id),
         %Planes{} = conn <- Planes.connection(:jetstream),
         {:ok, clip} <- metadata(conn, "#{session.user_id}/#{event_id}"),
         {:ok, chunk} <- next_chunk(conn, clip.topic, 1),
         true <- byte_size(chunk.data) <= clip.size do
      stream(conn, req, headers, {clip, chunk})
    else
      _ ->
        Logger.warning("TTS clip unavailable", user_id: session.user_id)
        {:ok, :cowboy_req.reply(404, headers, req), nil}
    end
  end

  defp valid_event_id?(event_id),
    do: String.valid?(event_id) and :binary.match(event_id, ["/", <<0>>]) == :nomatch

  defp metadata(conn, key) do
    subject = "$O.#{@bucket}.M." <> Base.url_encode64(key)

    with {:ok, %{data: data}} <- get(conn, %{last_by_subj: subject}, subject),
         {:ok,
          %{
            "name" => ^key,
            "bucket" => @bucket,
            "nuid" => nuid,
            "size" => size,
            "chunks" => chunks,
            "digest" => "SHA-256=" <> digest
          } = meta} <- Jason.decode(data),
         false <- Map.get(meta, "deleted", false),
         true <- nuid?(nuid),
         true <- positive?(size),
         true <- positive?(chunks),
         {:ok, hash} when byte_size(hash) == 32 <- Base.url_decode64(digest, padding: false) do
      {:ok, %{topic: "$O.#{@bucket}.C.#{nuid}", size: size, left: chunks, digest: hash}}
    else
      _ -> :error
    end
  end

  defp nuid?(nuid) when is_binary(nuid), do: Regex.match?(~r/\A[A-Za-z0-9_-]+\z/, nuid)
  defp nuid?(_nuid), do: false

  defp positive?(n) when is_integer(n), do: n > 0
  defp positive?(_n), do: false

  defp get(conn, query, subject) do
    with {:ok, %{"message" => %{"data" => data, "seq" => seq, "subject" => ^subject}}} <-
           Nats.jetstream_request(conn, @get, query),
         {:ok, bytes} <- Base.decode64(data) do
      {:ok, %{data: bytes, seq: seq}}
    else
      _ -> :error
    end
  end

  defp next_chunk(conn, topic, seq), do: get(conn, %{seq: seq, next_by_subj: topic}, topic)

  defp stream(conn, req, headers, {clip, chunk}) do
    headers =
      Map.merge(headers, %{
        "content-type" => "audio/wav",
        "content-length" => Integer.to_string(clip.size),
        "x-content-type-options" => "nosniff"
      })

    req = :cowboy_req.stream_reply(200, headers, req)
    stream_chunk(conn, req, Map.put(clip, :sha, :crypto.hash_init(:sha256)), chunk)
    {:ok, req, nil}
  end

  defp stream_chunk(conn, req, clip, chunk) do
    size = clip.size - byte_size(chunk.data)
    sha = :crypto.hash_update(clip.sha, chunk.data)

    case chunk_step(clip.left, size) do
      :last -> finish_chunk(req, clip, sha, chunk)
      :more -> stream_next(conn, req, %{clip | size: size, left: clip.left - 1, sha: sha}, chunk)
      :invalid -> interrupted()
    end
  end

  defp chunk_step(1, 0), do: :last
  defp chunk_step(left, size) when left > 1 and size > 0, do: :more
  defp chunk_step(_left, _size), do: :invalid

  defp finish_chunk(req, clip, sha, chunk) do
    if :crypto.hash_final(sha) == clip.digest do
      :cowboy_req.stream_body(chunk.data, :fin, req)
    else
      interrupted()
    end
  end

  defp stream_next(conn, req, clip, chunk) do
    with :ok <- :cowboy_req.stream_body(chunk.data, :nofin, req),
         {:ok, next} <- next_chunk(conn, clip.topic, chunk.seq + 1) do
      stream_chunk(conn, req, clip, next)
    else
      _ -> interrupted()
    end
  end

  defp interrupted do
    Logger.warning("TTS clip read interrupted")
    exit(:shutdown)
  end
end
