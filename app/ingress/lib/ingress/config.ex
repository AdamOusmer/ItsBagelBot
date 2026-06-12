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

  def broadcaster_status_subject,
    do: Application.fetch_env!(:ingress, :broadcaster_status_subject)

  def broadcaster_status_timeout_ms,
    do: Application.get_env(:ingress, :broadcaster_status_timeout_ms, 2_000)

  def broadcaster_cache_ttl_ms,
    do: Application.get_env(:ingress, :broadcaster_cache_ttl_ms, 300_000)

  def nats, do: Application.fetch_env!(:ingress, :nats)
end
