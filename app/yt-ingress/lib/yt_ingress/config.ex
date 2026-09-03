# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.Config do
  @moduledoc """
  Thin accessors over application env. Everything here is set once at boot by
  `config/runtime.exs`.

  This module keeps only the cross-cutting settings: lane routing, broadcaster
  status lookups, tracing and the NATS connection settings. Subsystem tuning
  lives in the per-concern module (`YtIngress.Config.YouTube`) so a tunable
  added for one subsystem no longer touches this shared file.
  """

  def cluster_topologies, do: Application.get_env(:yt_ingress, :cluster_topologies, [])

  def special_user_ids, do: Application.get_env(:yt_ingress, :special_user_ids, MapSet.new())

  def lane_subject(:premium), do: Application.fetch_env!(:yt_ingress, :lane_subject_premium)
  def lane_subject(:standard), do: Application.fetch_env!(:yt_ingress, :lane_subject_standard)
  def lane_subject(:stream), do: Application.fetch_env!(:yt_ingress, :lane_subject_stream)

  def invalidation_subject, do: Application.fetch_env!(:yt_ingress, :invalidation_subject)

  def broadcaster_status_subject,
    do: Application.fetch_env!(:yt_ingress, :broadcaster_status_subject)

  def broadcaster_status_timeout_ms,
    do: Application.get_env(:yt_ingress, :broadcaster_status_timeout_ms, 2_000)

  def broadcaster_cache_ttl_ms,
    do: Application.get_env(:yt_ingress, :broadcaster_cache_ttl_ms, 300_000)

  # Size guard: chat text past this many bytes is malformed/abuse and dropped.
  # A well-formed YouTube live chat line is well under this; the ceiling is
  # generous.
  def max_chat_text_bytes,
    do: Application.get_env(:yt_ingress, :max_chat_text_bytes, 4_096)

  # One in N events receives transaction and trace headers. Zero disables
  # per-event tracing; one is reserved for controlled diagnostics.
  def trace_sample_rate,
    do: Application.get_env(:yt_ingress, :trace_sample_rate, 1_024)

  # Gnat connection_settings (a leaf-first list of server maps) for the two
  # planes: :nats is the yt_ingress RPC account, :nats_bus the shared BUS
  # account that carries the youtube.ingress.* firehose.
  def nats, do: Application.fetch_env!(:yt_ingress, :nats)
  def nats_bus, do: Application.fetch_env!(:yt_ingress, :nats_bus)
end
