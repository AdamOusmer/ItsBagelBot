# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Config.Publish do
  def ack_timeout_ms,
    do: Application.get_env(:ingress, :publish_ack_timeout_ms, 2_000)

  def call_timeout_ms, do: max(div(ack_timeout_ms(), 2), 250)

  def attempts,
    do: Application.get_env(:ingress, :publish_attempts, 3)

  def max_pending,
    do: Application.get_env(:ingress, :publish_max_pending, 16_384)

  def batch_size,
    do: Application.get_env(:ingress, :publish_batch_size, 128)

  def batch_wait_ms,
    do: Application.get_env(:ingress, :publish_batch_wait_ms, 1)

  def send_concurrency,
    do: Application.get_env(:ingress, :publish_send_concurrency, 22) |> max(1) |> min(32)

  def wire,
    do: Application.get_env(:ingress, :publish_wire, :atomic)

  def batch_inflight,
    do: Application.get_env(:ingress, :publish_batch_inflight, 8)

  def batch_hold_ms,
    do: Application.get_env(:ingress, :publish_batch_hold_ms, 10_000)

  def connections,
    do: Application.get_env(:ingress, :publish_connections, System.schedulers_online())
end
