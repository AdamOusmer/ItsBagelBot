# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.ObjectStore do
  @moduledoc "Reads NATS object-store objects with size and SHA-256 verification."

  alias Platform.Nats.Planes

  @default_timeout_ms 5_000

  def info(connection, bucket, key, opts \\ []) when is_binary(bucket) and is_binary(key) do
    subject = "$O.#{bucket}.M." <> Base.url_encode64(key)
    timeout = Keyword.get(opts, :timeout, @default_timeout_ms)

    with true <- token?(bucket),
         {:ok, %{data: data}} <-
           get({connection, timeout}, bucket, %{last_by_subj: subject}, subject),
         {:ok,
          %{
            "name" => ^key,
            "bucket" => ^bucket,
            "nuid" => nuid,
            "size" => size,
            "chunks" => chunks,
            "digest" => "SHA-256=" <> digest
          } = meta} <- Jason.decode(data),
         false <- Map.get(meta, "deleted", false),
         true <- token?(nuid),
         true <- positive?(size),
         true <- positive?(chunks),
         {:ok, hash} when byte_size(hash) == 32 <- decode_digest(digest) do
      {:ok,
       %{
         connection: connection,
         timeout: timeout,
         bucket: bucket,
         key: key,
         topic: "$O.#{bucket}.C.#{nuid}",
         size: size,
         chunks: chunks,
         digest: hash
       }}
    else
      {:error, reason} when is_atom(reason) -> {:error, reason}
      _ -> {:error, :invalid_object}
    end
  end

  def stream(info, acc, fun) do
    state = %{seq: 1, left: info.chunks, remaining: info.size, sha: :crypto.hash_init(:sha256)}
    read(info, state, {acc, fun})
  end

  defp read(info, state, {acc, fun}) do
    case step(info, state, acc, fun) do
      {:ok, state, acc} -> continue(info, state, {acc, fun})
      {:error, _reason, _acc} = failure -> failure
    end
  end

  defp step(info, state, acc, fun) do
    query = %{seq: state.seq, next_by_subj: info.topic}

    with {:ok, chunk} <- get({info.connection, info.timeout}, info.bucket, query, info.topic),
         {:ok, state} <- advance(state, chunk),
         :ok <- verify(state, info),
         {:ok, acc} <- fun.(chunk.data, acc) do
      {:ok, state, acc}
    else
      {:error, reason} -> {:error, reason, acc}
      {:error, _reason, _acc} = failure -> failure
    end
  end

  defp verify(%{left: 0, sha: sha}, info) do
    if :crypto.hash_final(sha) == info.digest, do: :ok, else: {:error, :digest_mismatch}
  end

  defp verify(_state, _info), do: :ok

  defp continue(_info, %{left: 0}, {acc, _fun}), do: {:ok, acc}
  defp continue(info, state, sink), do: read(info, state, sink)

  defp advance(state, chunk) do
    next = %{
      seq: chunk.seq + 1,
      left: state.left - 1,
      remaining: state.remaining - byte_size(chunk.data),
      sha: :crypto.hash_update(state.sha, chunk.data)
    }

    if consistent?(next), do: {:ok, next}, else: {:error, :size_mismatch}
  end

  defp consistent?(%{left: 0, remaining: 0}), do: true
  defp consistent?(%{left: left, remaining: remaining}), do: left > 0 and remaining > 0

  defp get({connection, timeout}, bucket, query, subject) do
    api = "$JS.API.STREAM.MSG.GET.OBJ_" <> bucket
    request = {api, Jason.encode!(query), [receive_timeout: timeout]}

    with {:ok, %{body: body}} <- Planes.jetstream_request(request, connection),
         {:ok, %{"message" => %{"data" => data, "seq" => seq, "subject" => ^subject}}} <-
           Jason.decode(body),
         {:ok, bytes} <- Base.decode64(data) do
      {:ok, %{data: bytes, seq: seq}}
    else
      {:error, reason} when is_atom(reason) -> {:error, reason}
      _ -> {:error, :not_found}
    end
  end

  defp decode_digest(digest),
    do: digest |> String.trim_trailing("=") |> Base.url_decode64(padding: false)

  defp token?(value) when is_binary(value), do: Regex.match?(~r/\A[A-Za-z0-9_-]+\z/, value)
  defp token?(_value), do: false

  defp positive?(n) when is_integer(n), do: n > 0
  defp positive?(_n), do: false
end
