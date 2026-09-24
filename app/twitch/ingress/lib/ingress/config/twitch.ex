# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Config.Twitch do
  def client_id, do: Application.fetch_env!(:ingress, :twitch_client_id)
  def client_secret, do: Application.fetch_env!(:ingress, :twitch_client_secret)
  def conduit_id, do: Application.get_env(:ingress, :twitch_conduit_id)
  def conduit_shard_count, do: Application.fetch_env!(:ingress, :conduit_shard_count)
  def eventsub_url, do: Application.fetch_env!(:ingress, :eventsub_url)

  def max_shards, do: Application.get_env(:ingress, :max_shards, 11)
end
