# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher.AckPath do
  @inbox_prefix "_INBOX.ingresspub."

  @type ref :: {:single | :batch_start | :batch_commit, pos_integer()}

  @spec new_inbox() :: {binary(), String.t()}
  def new_inbox do
    token = :crypto.strong_rand_bytes(9) |> Base.url_encode64(padding: false)
    {token, @inbox_prefix <> token <> "."}
  end

  @spec subscription(String.t()) :: String.t()
  def subscription(prefix), do: prefix <> ">"

  @spec single(String.t(), pos_integer()) :: String.t()
  def single(prefix, id), do: prefix <> "s." <> Integer.to_string(id)

  @spec batch_start(String.t(), pos_integer()) :: String.t()
  def batch_start(prefix, id), do: prefix <> "bs." <> Integer.to_string(id)

  @spec batch_commit(String.t(), pos_integer()) :: String.t()
  def batch_commit(prefix, id), do: prefix <> "bc." <> Integer.to_string(id)

  @spec parse(String.t(), String.t()) :: ref() | nil
  def parse(topic, prefix) do
    plen = byte_size(prefix)

    case topic do
      <<^prefix::binary-size(^plen), "bs.", id::binary>> ->
        tagged(:batch_start, id)

      <<^prefix::binary-size(^plen), "bc.", id::binary>> ->
        tagged(:batch_commit, id)

      <<^prefix::binary-size(^plen), "s.", id::binary>> ->
        tagged(:single, id)

      <<^prefix::binary-size(^plen), id::binary>> ->
        tagged(:single, id)

      _ ->
        nil
    end
  end

  defp tagged(tag, id) do
    case Integer.parse(id) do
      {value, ""} -> {tag, value}
      _ -> nil
    end
  end
end
