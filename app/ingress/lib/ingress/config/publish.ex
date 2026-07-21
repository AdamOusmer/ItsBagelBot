defmodule Ingress.Config.Publish do
  @moduledoc """
  Lane-publisher tuning (see `Ingress.Nats.Publisher` / `PublisherPool`).
  Thin accessors over application env, set once at boot by `config/runtime.exs`
  under the same `:publish_*` keys as before the split.
  """

  # How long an outstanding publish waits for its PubAck before
  # the collector reconciles it (see Ingress.Nats.Publisher).
  def ack_timeout_ms,
    do: Application.get_env(:ingress, :publish_ack_timeout_ms, 2_000)

  # Total delivery attempts (first try + retries) for one lane publish.
  # Only definite negative acknowledgements are retried; ambiguous timeouts
  # are dropped because ingress intentionally does not use Nats-Msg-Id.
  def attempts,
    do: Application.get_env(:ingress, :publish_attempts, 3)

  # Ceiling on outstanding (queued or un-acked) lane events per publisher shard.
  # This is the publisher's backpressure valve: at the measured PubAck latency
  # it sets peak publish throughput (publish_connections × max_pending /
  # ack_latency), and it bounds the memory held for in-flight events when the
  # broker stalls. Once reached, further publishes routed to that shard are shed
  # as overloaded rather than buffered without limit.
  def max_pending,
    do: Application.get_env(:ingress, :publish_max_pending, 16_384)

  # Scheduler-local cohort shape. Each member still receives an official
  # per-message PubAck; the cohort only amortizes publisher scheduling.
  def batch_size,
    do: Application.get_env(:ingress, :publish_batch_size, 128)

  def batch_wait_ms,
    do: Application.get_env(:ingress, :publish_batch_wait_ms, 1)

  # Persistent send lanes feeding one Gnat connection. Gnat currently
  # coalesces the active call plus ten queued calls into one socket write; two
  # windows keep its mailbox fed between replies.
  def send_concurrency,
    do: Application.get_env(:ingress, :publish_send_concurrency, 22) |> max(1) |> min(32)

  # Wire protocol for one flushed cohort. :single publishes every event with
  # its own JetStream PubAck (the long-standing path). :atomic writes the
  # cohort as one ADR-050 atomic batch (NATS 2.14): a single commit PubAck
  # per cohort instead of one per event. Default stays :single so the mode
  # ships dark and is enabled by INGRESS_PUBLISH_WIRE=atomic only after the R3
  # acceptance matrix proves its loss and latency behavior.
  def wire,
    do: Application.get_env(:ingress, :publish_wire, :single)

  # Ceiling on unresolved atomic batches per shard. The broker allows 50
  # in-flight batches per stream across ALL publishers, so the fleet budget is
  # shards × pods × this cap; overflow cohorts fall back to per-message
  # publishes rather than risking broker-side batch rejection.
  def batch_inflight,
    do: Application.get_env(:ingress, :publish_batch_inflight, 4)

  # Number of independent BUS connections (each with its own ack collector) the
  # lane firehose is sharded across. Every publish is a GenServer.call into one
  # Gnat connection process and every PubAck is handled by one collector, so a
  # single connection tops out well below a 150-200k/s target; sharding spreads
  # both across N processes and cores. Keep this equal to the online scheduler
  # count so every connection carries one identical scheduler-local path.
  def connections,
    do: Application.get_env(:ingress, :publish_connections, System.schedulers_online())
end
