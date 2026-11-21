# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Clips do
  require Logger

  alias ControlsGateway.Tts.Nats

  @bucket "TTS_CLIPS"
  @get "$JS.API.STREAM.MSG.GET.OBJ_TTS_CLIPS"

  def reply(req, session, headers) do
    event_id = :cowboy_req.binding(:event_id, req)
    conn = Nats.connection()

    with true <- valid_event_id?(event_id),
         {:ok, clip} <- metadata(conn, "#{session.user_id}/#{event_id}"),
         {:ok, chunk} <- next_chunk(conn, clip.topic, 1),
         true <- byte_size(chunk.data) <= clip.size do
      stream(conn, req, headers, {clip, chunk})
    else
      _ ->
        Logger.debug("TTS clip unavailable")
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
         true <- valid_metadata?(nuid, size, chunks),
         {:ok, hash} when byte_size(hash) == 32 <- Base.url_decode64(digest, padding: false) do
      {:ok, %{topic: "$O.#{@bucket}.C.#{nuid}", size: size, left: chunks, digest: hash}}
    else
      _ -> :error
    end
  end

  defp valid_metadata?(nuid, size, chunks)
       when is_binary(nuid) and is_integer(size) and is_integer(chunks) do
    size > 0 and chunks > 0 and Regex.match?(~r/\A[A-Za-z0-9]+\z/, nuid)
  end

  defp valid_metadata?(_nuid, _size, _chunks), do: false

  defp get(conn, query, subject) do
    with {:ok, %{"message" => %{"data" => data, "seq" => seq, "subject" => ^subject}}} <-
           Nats.request(conn, @get, query),
         {:ok, bytes} <- Base.decode64(data) do
      {:ok, %{data: bytes, seq: seq}}
    else
      _ -> :error
    end
  end

  defp next_chunk(conn, topic, seq), do: get(conn, %{seq: seq, next_by_subj: topic}, topic)

  defp stream(conn, req, headers, {clip, chunk}) do
    headers =
      Map.merge(headers, %{"content-type" => "audio/wav", "x-content-type-options" => "nosniff"})

    req = :cowboy_req.stream_reply(200, headers, req)
    stream_chunk(conn, req, Map.put(clip, :sha, :crypto.hash_init(:sha256)), chunk)
    {:ok, req, nil}
  end

  defp stream_chunk(conn, req, clip, chunk) do
    size = clip.size - byte_size(chunk.data)
    sha = :crypto.hash_update(clip.sha, chunk.data)

    case clip.left do
      1 ->
        if size == 0 and :crypto.hash_final(sha) == clip.digest do
          :cowboy_req.stream_body(chunk.data, :fin, req)
        else
          interrupted()
        end

      left ->
        with true <- size > 0,
             :ok <- :cowboy_req.stream_body(chunk.data, :nofin, req),
             {:ok, next} <- next_chunk(conn, clip.topic, chunk.seq + 1) do
          stream_chunk(conn, req, %{clip | size: size, left: left - 1, sha: sha}, next)
        else
          _ -> interrupted()
        end
    end
  end

  defp interrupted do
    Logger.warning("TTS clip read interrupted")
    exit(:shutdown)
  end
end
