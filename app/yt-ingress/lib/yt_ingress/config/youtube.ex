# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.Config.YouTube do
  @moduledoc """
  YouTube-specific subsystem tuning: the streamList gRPC endpoint, the watch
  list and its discovery cadence, static credential fallbacks and the token
  RPC contract.
  """

  def grpc_host, do: Application.get_env(:yt_ingress, :youtube_grpc_host, "youtube.googleapis.com")

  def grpc_port, do: Application.get_env(:yt_ingress, :youtube_grpc_port, 443)

  # Static credential fallback so local development can stream a chat without
  # the users service behind it. Production resolves per-channel OAuth access
  # tokens over NATS RPC (`token_subject/0`).
  def api_key, do: Application.get_env(:yt_ingress, :youtube_api_key)

  # Channels to watch, as a set of YouTube channel IDs. The long-term source
  # of truth is the users service over NATS RPC; until that endpoint ships the
  # watch list is provisioned through YOUTUBE_CHANNEL_IDS.
  def channel_ids, do: Application.get_env(:yt_ingress, :channel_ids, MapSet.new())

  # Live chat IDs pinned via env that bypass broadcast discovery entirely
  # (development escape hatch).
  def pinned_live_chat_ids, do: Application.get_env(:yt_ingress, :pinned_live_chat_ids, [])

  # Silence budget before a chat session presumes its stream dead.
  def chat_idle_timeout_seconds,
    do: Application.get_env(:yt_ingress, :chat_idle_timeout_seconds, 300)

  # How often the watcher re-checks each watched channel for an active
  # broadcast. liveBroadcasts.list costs 1 quota unit, so 30s polling is
  # 2,880 units/channel/day inside the default 10k/day project budget.
  def watcher_poll_seconds,
    do: Application.get_env(:yt_ingress, :watcher_poll_seconds, 30)

  def data_api_url,
    do: Application.get_env(:yt_ingress, :youtube_data_api_url, "https://www.googleapis.com/youtube/v3")

  def token_subject, do: Application.fetch_env!(:yt_ingress, :token_subject)
  def token_timeout_ms, do: Application.get_env(:yt_ingress, :token_timeout_ms, 2_000)

  # A cached access token is refreshed this many seconds ahead of its expiry.
  def token_refresh_margin_seconds,
    do: Application.get_env(:yt_ingress, :token_refresh_margin_seconds, 60)

  # Request-reply subject answered by YtIngress.AdminRpc with a cluster-wide
  # snapshot of watchers and open chat streams.
  def admin_subject, do: Application.fetch_env!(:yt_ingress, :admin_subject)

  # Side-effect-free admin latency probe shared by every RPC-serving service.
  def rpc_health_subject, do: Application.fetch_env!(:yt_ingress, :rpc_health_subject)
end
