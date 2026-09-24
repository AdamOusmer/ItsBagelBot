# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Config do
  @hot_path_key {__MODULE__, :hot_path}

  @doc false
  def install_hot_path do
    :persistent_term.put(@hot_path_key, hot_path_from_env())

    :ok
  end

  def hot_path do
    :persistent_term.get(@hot_path_key, nil) || hot_path_from_env()
  end

  defp hot_path_from_env do
    %{
      special_user_ids: special_user_ids(),
      max_chat_text_bytes: max_chat_text_bytes(),
      lane_subjects: %{
        premium: lane_subject(:premium),
        standard: lane_subject(:standard),
        stream: lane_subject(:stream)
      }
    }
  end

  @doc false
  def uninstall_hot_path do
    :persistent_term.erase(@hot_path_key)
    :ok
  end

  def hot_special_user_ids do
    hot_path().special_user_ids
  end

  def hot_max_chat_text_bytes do
    hot_path().max_chat_text_bytes
  end

  def hot_lane_subject(lane) do
    Map.fetch!(hot_path().lane_subjects, lane)
  end

  def cluster_topologies, do: Application.get_env(:ingress, :cluster_topologies, [])

  def special_user_ids, do: Application.get_env(:ingress, :special_user_ids, MapSet.new())

  def lane_subject(:premium), do: Application.fetch_env!(:ingress, :lane_subject_premium)
  def lane_subject(:standard), do: Application.fetch_env!(:ingress, :lane_subject_standard)
  def lane_subject(:stream), do: Application.fetch_env!(:ingress, :lane_subject_stream)

  def invalidation_subject, do: Application.fetch_env!(:ingress, :invalidation_subject)

  def broadcaster_status_subject,
    do: Application.fetch_env!(:ingress, :broadcaster_status_subject)

  def broadcaster_status_timeout_ms,
    do: Application.get_env(:ingress, :broadcaster_status_timeout_ms, 2_000)

  def broadcaster_cache_ttl_ms,
    do: Application.get_env(:ingress, :broadcaster_cache_ttl_ms, 300_000)

  def max_chat_text_bytes,
    do: Application.get_env(:ingress, :max_chat_text_bytes, 4_096)

  def trace_sample_rate,
    do: Application.get_env(:ingress, :trace_sample_rate, 1_024)

  def nats, do: Application.fetch_env!(:ingress, :nats)
  def nats_bus, do: Application.fetch_env!(:ingress, :nats_bus)
end
