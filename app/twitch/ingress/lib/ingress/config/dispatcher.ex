# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Config.Dispatcher do
  def max_running,
    do: Application.get_env(:ingress, :dispatcher_max_running, 512)

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
