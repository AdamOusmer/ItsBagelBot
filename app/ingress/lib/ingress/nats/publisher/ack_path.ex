# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary and unlicensed. See LICENSE.md.

defmodule Ingress.Nats.Publisher.AckPath do
  @moduledoc """
  The reply-inbox naming scheme every publisher wire acknowledges over.

  One shard owns one random inbox prefix and subscribes to its whole subtree;
  the suffix says which pending row a reply belongs to and which wire owns it:

      <prefix>s.<id>   — one single-wire publish, one JetStream PubAck
      <prefix>bs.<id>  — an atomic batch's opening message (rejections only)
      <prefix>bc.<id>  — an atomic batch's commit message (the cohort PubAck)

  Building and parsing sit together so a new wire cannot invent a tag the
  collector does not route. `parse/2` also accepts a bare `<prefix><id>`, the
  pre-tag single form, so replies already in flight across a rolling upgrade
  still land on their row.
  """

  @inbox_prefix "_INBOX.ingresspub."

  @type ref :: {:single | :batch_start | :batch_commit, pos_integer()}

  @doc """
  Mints this shard's inbox: an unguessable token and the prefix built from it.
  The token doubles as the atomic wire's batch-id namespace.
  """
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
        # Backwards-compatible parser for in-flight replies across a rolling
        # upgrade and for the public id_from_topic/2 contract.
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
