defmodule Ingress.Config.Publish do
  @moduledoc """
  Lane-publisher tuning (see `Ingress.Nats.Publisher` / `PublisherPool`).
  Thin accessors over application env, set once at boot by `config/runtime.exs`
  under the same `:publish_*` keys as before the split.

  ## Atomic-wire blast radius

  `wire/0` picks how much an unlucky cohort costs. Both wires obey the same
  dedup-free rule — an ambiguous outcome (ack timeout, malformed reply, a send
  error after part of the write is on the socket) drops rather than replays —
  but they drop different amounts:

    * `:single` — one ambiguous outcome drops exactly **1** event.
    * `:atomic` — one ambiguous outcome drops the **whole cohort**, up to
      `batch_size/0` (128) events, because the cohort is one pending row
      resolved by one commit PubAck.

  Nothing is lost silently: every dropped event is counted in
  `Nats/PublishFailed`. The operator signal is the *shape* of that counter,
  not its existence. On `:atomic` it climbs in cohort-sized steps (a multiple
  of the flushed cohort size, up to 128 at a time); on `:single` it climbs one
  event at a time. A step-shaped `Nats/PublishFailed` is the cue to set
  `INGRESS_PUBLISH_WIRE=single`, which trades that blast radius back for one
  RAFT quorum round trip per event.
  """

  # How long an outstanding publish waits for its PubAck before
  # the collector reconciles it (see Ingress.Nats.Publisher).
  def ack_timeout_ms,
    do: Application.get_env(:ingress, :publish_ack_timeout_ms, 2_000)

  # Ceiling on one blocking wire write. `Gnat.pub/4` is a `GenServer.call` with
  # a hardcoded 5s default, and the collector processes no PubAcks and no sweep
  # ticks while it blocks — so on a wedged socket write (TLS renegotiation, a
  # full kernel send buffer, the server's slow-consumer write deadline) the
  # default outlives the 2s ack budget and the tick that resumes afterwards
  # finds a whole window already past its deadline. Half the ack budget makes a
  # stalled connection surface as {:error, :not_connected} with a full budget
  # of headroom left, which is a definite outcome rather than a mass timeout.
  # Derived, not configurable: the two clocks must not be tunable apart.
  def call_timeout_ms, do: max(div(ack_timeout_ms(), 2), 250)

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

  # Wire protocol for one flushed cohort. :atomic writes the cohort as one
  # ADR-050 atomic batch (NATS 2.14): a single commit PubAck per cohort instead
  # of one per event. That is the default because the lane stream is
  # replicated — every per-message PubAck on an R3 stream costs a RAFT quorum
  # round trip, and the atomic wire amortizes that cost across the whole
  # cohort. :single publishes every event with its own JetStream PubAck: the
  # long-standing path, kept as the compatibility fallback (a broker without
  # batch support, or a stream with allow_atomic off) and selectable with
  # INGRESS_PUBLISH_WIRE=single. Both wires are structurally dedup-free. See
  # the blast-radius note in this module's doc before flipping it.
  def wire,
    do: Application.get_env(:ingress, :publish_wire, :atomic)

  # Ceiling on unresolved atomic batches per shard. This is a latency × rate
  # window (Little's law), NOT a guard against the broker's per-stream cap:
  # concurrent unresolved batches = flush rate × commit-ack latency.
  # `ensure_flush_scheduled/1` arms its timer at batch_wait_ms = 1, so a busy
  # shard flushes up to ~1000 cohorts/s and the window is
  #
  #     1000 cohorts/s × commit-ack latency
  #
  # so 8 covers an 8 ms commit ack at the full flush rate. The number is chosen
  # by the FLEET budget rather than by that latency, because the broker caps
  # concurrently-open atomic batches at 50 PER STREAM
  # (streamDefaultMaxAtomicBatchInflightPerStream) and every shard opens against
  # the same TWITCH_INGRESS:
  #
  #     3 replicas (deploy/k8s/twitch-ingress.yaml)
  #       × 2 schedulers (`+S 2:2`)
  #       × 8 = 48 concurrently open batches, against 50.
  #
  # 50 is what the hub actually runs: deploy/k8s/nats-server.conf ships no
  # `limits { batch { … } }` block on purpose, because JetStreamLimits is not
  # hot-reloadable and one such key would wedge every later SIGHUP.
  #
  # Sizing past it does not buy the extra window. An over-cap open is answered
  # with a definite 10210/429 — nothing was stored — so the cohort re-drives per
  # message and lands in Nats/PublishBatchFallback. That is not data loss, but it
  # is one extra round trip plus a quorum ack per event, i.e. the exact cost this
  # wire exists to remove, arriving precisely when every shard is saturated at
  # once and the amortization is worth most. A window the broker refuses to
  # honour is not a window.
  #
  # The cost of staying under the cap, stated plainly: 8 ms of Little's-law
  # coverage instead of 16. A shard whose commit acks run slower than that fills
  # the local window and bypasses to per-message publishes. Raising the broker's
  # max_inflight_per_stream is the correct lever if that shows up — and note it
  # is shared with the fast-ingest ceiling, so raising it is not free either.
  #
  # This keeps the two signals distinct. Nats/PublishBatchBypassed means the
  # LOCAL window filled: a commit-latency alarm. Nats/PublishBatchFallback
  # climbing with 429s means the FLEET budget overran the broker cap: the
  # arithmetic above is stale, so check the replica count and `+S` first. There
  # is no adaptive sizing on purpose — a feedback loop here would trade a
  # predictable, observable fallback for one that oscillates under load.
  def batch_inflight,
    do: Application.get_env(:ingress, :publish_batch_inflight, 8)

  # How long a swept atomic batch keeps its LOCAL in-flight slot after its
  # events have been resolved. Nothing on the wire cancels an open batch: only
  # the broker's inactivity timer closes it (jetstream.limits.batch.timeout,
  # default streamDefaultMaxBatchTimeout = 10s), and a client-side discard
  # sends nothing at all. Releasing the slot at ack_timeout_ms would let one
  # shard recycle its whole cap every ~2s while the broker still holds each
  # abandoned batch — and its staged cohort in the R3 leader's RAM — for 10s,
  # so local admission would run ~5× over the broker budget exactly when the
  # broker is already stalled. Keep this equal to the deployed broker timeout.
  def batch_hold_ms,
    do: Application.get_env(:ingress, :publish_batch_hold_ms, 10_000)

  # Number of independent BUS connections (each with its own ack collector) the
  # lane firehose is sharded across. Every publish is a GenServer.call into one
  # Gnat connection process and every PubAck is handled by one collector, so a
  # single connection tops out well below a 150-200k/s target; sharding spreads
  # both across N processes and cores. Keep this equal to the online scheduler
  # count so every connection carries one identical scheduler-local path.
  def connections,
    do: Application.get_env(:ingress, :publish_connections, System.schedulers_online())
end
