defmodule Ingress.Config do
  @moduledoc """
  Thin accessors over application env. Everything here is set once at boot by
  `config/runtime.exs`.
  """

  def cluster_topologies, do: Application.get_env(:ingress, :cluster_topologies, [])

  def twitch_client_id, do: Application.fetch_env!(:ingress, :twitch_client_id)
  def twitch_client_secret, do: Application.fetch_env!(:ingress, :twitch_client_secret)
  def twitch_conduit_id, do: Application.get_env(:ingress, :twitch_conduit_id)
  def conduit_shard_count, do: Application.fetch_env!(:ingress, :conduit_shard_count)
  def eventsub_url, do: Application.fetch_env!(:ingress, :eventsub_url)

  def special_user_ids, do: Application.get_env(:ingress, :special_user_ids, MapSet.new())

  def lane_subject(:premium), do: Application.fetch_env!(:ingress, :lane_subject_premium)
  def lane_subject(:standard), do: Application.fetch_env!(:ingress, :lane_subject_standard)
  def lane_subject(:stream), do: Application.fetch_env!(:ingress, :lane_subject_stream)

  def invalidation_subject, do: Application.fetch_env!(:ingress, :invalidation_subject)

  def admin_subject, do: Application.fetch_env!(:ingress, :admin_subject)

  # Subject for manual shard-count scaling: body {"count": N}.
  def scale_subject, do: Application.fetch_env!(:ingress, :scale_subject)

  # Subject for toggling the load-based autoscaler: body {"enabled": true|false}.
  def autoscale_subject, do: Application.fetch_env!(:ingress, :autoscale_subject)

  # Subject for live conduit id query: body {}, replies {"conduit_id": "<uuid>"}.
  def conduit_subject, do: Application.fetch_env!(:ingress, :conduit_subject)

  # Hard ceiling on shard count; the autoscaler and manual target are both
  # clamped to this value so a runaway load spike cannot blow the conduit cap.
  def max_shards, do: Application.get_env(:ingress, :max_shards, 20)

  def broadcaster_status_subject,
    do: Application.fetch_env!(:ingress, :broadcaster_status_subject)

  def broadcaster_status_timeout_ms,
    do: Application.get_env(:ingress, :broadcaster_status_timeout_ms, 2_000)

  def broadcaster_cache_ttl_ms,
    do: Application.get_env(:ingress, :broadcaster_cache_ttl_ms, 300_000)

  # How long identical non-command chat is coalesced before the cohort is
  # flushed (see Ingress.Squash).
  def squash_window_ms,
    do: Application.get_env(:ingress, :squash_window_ms, 2_000)

  # A cohort this large flushes early instead of waiting for the window, to
  # bound the event size under a raid.
  def squash_max_senders,
    do: Application.get_env(:ingress, :squash_max_senders, 500)

  # How often the squash sweep runs; keep it well under the window so cohorts
  # flush promptly after their window closes.
  def squash_sweep_ms,
    do: Application.get_env(:ingress, :squash_sweep_ms, 500)

  # Size guard: chat text past this many bytes is malformed/abuse and dropped.
  # A well-formed Twitch line is <= 500 chars; the ceiling is generous.
  def max_chat_text_bytes,
    do: Application.get_env(:ingress, :max_chat_text_bytes, 4_096)

  def dispatcher_max_running,
    do: Application.get_env(:ingress, :dispatcher_max_running, 64)

  def dispatcher_max_queue,
    do: Application.get_env(:ingress, :dispatcher_max_queue, 2_000)

  # Gnat connection_settings (a leaf-first list of server maps) for the two
  # planes: :nats is the twitch_ingress RPC account, :nats_bus the shared BUS
  # account that carries the twitch.ingress.* firehose.
  def nats, do: Application.fetch_env!(:ingress, :nats)
  def nats_bus, do: Application.fetch_env!(:ingress, :nats_bus)
end
