# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Config.Dispatcher do
  def max_running,
    do: Application.get_env(:ingress, :dispatcher_max_running, 512)

  def min_running,
    do: Application.get_env(:ingress, :dispatcher_min_running, 8)

  def pool_bounds(opts) do
    ceiling = Keyword.get(opts, :max_running, max_running())
    {min(Keyword.get(opts, :min_running, min_running()), ceiling), ceiling}
  end

  def scale_interval_ms,
    do: Application.get_env(:ingress, :dispatcher_scale_interval_ms, 100)

  def shrink_after_ms,
    do: Application.get_env(:ingress, :dispatcher_shrink_after_ms, 30_000)

  def retire_grace_ms,
    do: Application.get_env(:ingress, :dispatcher_retire_grace_ms, 1_000)

  def max_queue,
    do: Application.get_env(:ingress, :dispatcher_max_queue, 20_000)

  def max_per_broadcaster,
    do: Application.get_env(:ingress, :dispatcher_max_per_broadcaster, 2_048)

  def broadcaster_sweep_ms,
    do: Application.get_env(:ingress, :dispatcher_broadcaster_sweep_ms, 60_000)

  def completion_batch_size,
    do: Application.get_env(:ingress, :dispatcher_completion_batch_size, 4)

  def completion_flush_ms,
    do: Application.get_env(:ingress, :dispatcher_completion_flush_ms, 25)
end
