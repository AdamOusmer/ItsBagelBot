# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Config.Squash do
  def window_ms,
    do: Application.get_env(:ingress, :squash_window_ms, 2_000)

  def max_senders,
    do: Application.get_env(:ingress, :squash_max_senders, 500)

  def sweep_ms,
    do: Application.get_env(:ingress, :squash_sweep_ms, 500)

  def partitions,
    do: Application.get_env(:ingress, :squash_partitions, System.schedulers_online())
end
