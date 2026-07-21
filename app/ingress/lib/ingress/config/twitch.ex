defmodule Ingress.Config.Twitch do
  @moduledoc """
  Twitch application + conduit settings (see `Ingress.Twitch.API`, the shard
  sessions and the shard scaler). Thin accessors over application env, set once
  at boot by `config/runtime.exs` under the same keys as before the split.
  """

  def client_id, do: Application.fetch_env!(:ingress, :twitch_client_id)
  def client_secret, do: Application.fetch_env!(:ingress, :twitch_client_secret)
  def conduit_id, do: Application.get_env(:ingress, :twitch_conduit_id)
  def conduit_shard_count, do: Application.fetch_env!(:ingress, :conduit_shard_count)
  def eventsub_url, do: Application.fetch_env!(:ingress, :eventsub_url)

  # Hard ceiling on shard count; the autoscaler and manual target are both
  # clamped to this value so a runaway load spike cannot blow the conduit cap.
  def max_shards, do: Application.get_env(:ingress, :max_shards, 11)
end
