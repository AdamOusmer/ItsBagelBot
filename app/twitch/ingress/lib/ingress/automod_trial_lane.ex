# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AutomodTrialLane do
  alias Ingress.{Config, Metrics, Nats, Pipeline}

  @spec publish(map(), map()) :: :ok
  def publish(payload, meta) do
    with subject when is_binary(subject) <- Config.automod_trial_subject(),
         input when is_map(input) <- Pipeline.trial_chat_input(payload, meta, DateTime.utc_now()),
         :ok <- send_input(subject, input) do
      :ok
    else
      nil -> :ok
      {:error, reason} -> Metrics.count_drop("Trial/AutomodLaneDropped", reason)
    end

    :ok
  end

  # This runs inside trial admission: a stalled bus connection must cost this
  # copy, not the trial message already dispatched to Sesame.
  defp send_input(subject, input) do
    Nats.publish(subject, input)
  catch
    :exit, _reason -> {:error, :publish_exit}
  end
end
