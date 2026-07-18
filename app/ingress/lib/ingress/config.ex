defmodule Ingress.Config do
  @moduledoc """
  Thin accessors over application env. Everything here is set once at boot by
  `config/runtime.exs`.
  """

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

  def twitch_client_id, do: Application.fetch_env!(:ingress, :twitch_client_id)
  def twitch_client_secret, do: Application.fetch_env!(:ingress, :twitch_client_secret)
  def twitch_conduit_id, do: Application.get_env(:ingress, :twitch_conduit_id)
  def conduit_shard_count, do: Application.fetch_env!(:ingress, :conduit_shard_count)
  def eventsub_url, do: Application.fetch_env!(:ingress, :eventsub_url)

  def special_user_ids, do: Application.get_env(:ingress, :special_user_ids, MapSet.new())

  def lane_subject(:premium), do: Application.fetch_env!(:ingress, :lane_subject_premium)
  def lane_subject(:standard), do: Application.fetch_env!(:ingress, :lane_subject_standard)
  def lane_subject(:stream), do: Application.fetch_env!(:ingress, :lane_subject_stream)

  def invalidation_subject, do: Application.fetch_env!(:ingress, :invalidation_subject)

  def admin_subject, do: Application.fetch_env!(:ingress, :admin_subject)

  # Subject for manual shard-count scaling: body {"count": N}.
  def scale_subject, do: Application.fetch_env!(:ingress, :scale_subject)

  # Subject for toggling the load-based autoscaler: body {"enabled": true|false}.
  def autoscale_subject, do: Application.fetch_env!(:ingress, :autoscale_subject)

  # Subject for live conduit id query: body {}, replies {"conduit_id": "<uuid>"}.
  def conduit_subject, do: Application.fetch_env!(:ingress, :conduit_subject)

  # Side-effect-free admin RPC latency probe.
  def rpc_health_subject, do: Application.fetch_env!(:ingress, :rpc_health_subject)

  # Hard ceiling on shard count; the autoscaler and manual target are both
  # clamped to this value so a runaway load spike cannot blow the conduit cap.
  def max_shards, do: Application.get_env(:ingress, :max_shards, 11)

  def broadcaster_status_subject,
    do: Application.fetch_env!(:ingress, :broadcaster_status_subject)

  def broadcaster_status_timeout_ms,
    do: Application.get_env(:ingress, :broadcaster_status_timeout_ms, 2_000)

  def broadcaster_cache_ttl_ms,
    do: Application.get_env(:ingress, :broadcaster_cache_ttl_ms, 300_000)

  # How long identical non-command chat is coalesced before the cohort is
  # flushed (see Ingress.Squash).
  def squash_window_ms,
    do: Application.get_env(:ingress, :squash_window_ms, 2_000)

  # A cohort this large flushes early instead of waiting for the window, to
  # bound the event size under a raid.
  def squash_max_senders,
    do: Application.get_env(:ingress, :squash_max_senders, 500)

  # How often the squash sweep runs; keep it well under the window so cohorts
  # flush promptly after their window closes.
  def squash_sweep_ms,
    do: Application.get_env(:ingress, :squash_sweep_ms, 500)

  # Independent cohort owners. Hashing by cohort key preserves ordering within
  # one flood while allowing unrelated broadcaster/text pairs to fold in
  # parallel across all online schedulers.
  def squash_partitions,
    do: Application.get_env(:ingress, :squash_partitions, System.schedulers_online())

  # Size guard: chat text past this many bytes is malformed/abuse and dropped.
  # A well-formed Twitch line is <= 500 chars; the ceiling is generous.
  def max_chat_text_bytes,
    do: Application.get_env(:ingress, :max_chat_text_bytes, 4_096)

  def dispatcher_max_running,
    do: Application.get_env(:ingress, :dispatcher_max_running, 512)

  def dispatcher_max_queue,
    do: Application.get_env(:ingress, :dispatcher_max_queue, 20_000)

  # Caps how much of the pod-wide dispatcher budget (max_running + max_queue) a
  # single broadcaster can occupy at once, so a hot/raiding channel can't starve
  # every other broadcaster sharing the pod's shared worker pool.
  def dispatcher_max_per_broadcaster,
    do: Application.get_env(:ingress, :dispatcher_max_per_broadcaster, 2_048)

  # How often dead (zeroed) per-broadcaster counters are swept from the
  # dispatcher's ETS table, so a long-lived pod doesn't accumulate one entry per
  # distinct broadcaster ever seen.
  def dispatcher_broadcaster_sweep_ms,
    do: Application.get_env(:ingress, :dispatcher_broadcaster_sweep_ms, 60_000)

  # Workers fold completion bookkeeping in small local batches. Four keeps the
  # maximum unreported share below the default per-broadcaster admission cap
  # even when all 512 workers are active.
  def dispatcher_completion_batch_size,
    do: Application.get_env(:ingress, :dispatcher_completion_batch_size, 4)

  # Flush partial completion batches promptly when traffic goes idle.
  def dispatcher_completion_flush_ms,
    do: Application.get_env(:ingress, :dispatcher_completion_flush_ms, 25)

  # One in N notifications receives a transaction and trace headers. Zero
  # disables per-event tracing; one is reserved for controlled diagnostics.
  def trace_sample_rate,
    do: Application.get_env(:ingress, :trace_sample_rate, 1_024)

  # How long an outstanding publish waits for its PubAck before
  # the collector reconciles it (see Ingress.Nats.Publisher).
  def publish_ack_timeout_ms,
    do: Application.get_env(:ingress, :publish_ack_timeout_ms, 2_000)

  # Total delivery attempts (first try + retries) for one lane publish.
  # Only definite negative acknowledgements are retried; ambiguous timeouts
  # are dropped because ingress intentionally does not use Nats-Msg-Id.
  def publish_attempts,
    do: Application.get_env(:ingress, :publish_attempts, 3)

  # Ceiling on outstanding (queued or un-acked) lane events per publisher shard.
  # This is the publisher's backpressure valve: at the measured PubAck latency
  # it sets peak publish throughput (publish_connections × max_pending /
  # ack_latency), and it bounds the memory held for in-flight events when the
  # broker stalls. Once reached, further publishes routed to that shard are shed
  # as overloaded rather than buffered without limit.
  def publish_max_pending,
    do: Application.get_env(:ingress, :publish_max_pending, 16_384)

  # Scheduler-local cohort shape. Each member still receives an official
  # per-message PubAck; the cohort only amortizes publisher scheduling.
  def publish_batch_size,
    do: Application.get_env(:ingress, :publish_batch_size, 128)

  def publish_batch_wait_ms,
    do: Application.get_env(:ingress, :publish_batch_wait_ms, 1)

  # Persistent send lanes feeding one Gnat connection. Gnat currently
  # coalesces the active call plus ten queued calls into one socket write; two
  # windows keep its mailbox fed between replies.
  def publish_send_concurrency,
    do: Application.get_env(:ingress, :publish_send_concurrency, 22) |> max(1) |> min(32)

  # Wire protocol for one flushed cohort. :single publishes every event with
  # its own JetStream PubAck (the long-standing path). :atomic writes the
  # cohort as one ADR-050 atomic batch (NATS 2.14): a single commit PubAck
  # per cohort instead of one per event. Default stays :single so the mode
  # ships dark and is enabled by INGRESS_PUBLISH_WIRE=atomic only after the R3
  # acceptance matrix proves its loss and latency behavior.
  def publish_wire,
    do: Application.get_env(:ingress, :publish_wire, :single)

  # Ceiling on unresolved atomic batches per shard. The broker allows 50
  # in-flight batches per stream across ALL publishers, so the fleet budget is
  # shards × pods × this cap; overflow cohorts fall back to per-message
  # publishes rather than risking broker-side batch rejection.
  def publish_batch_inflight,
    do: Application.get_env(:ingress, :publish_batch_inflight, 4)

  # Number of independent BUS connections (each with its own ack collector) the
  # lane firehose is sharded across. Every publish is a GenServer.call into one
  # Gnat connection process and every PubAck is handled by one collector, so a
  # single connection tops out well below a 150-200k/s target; sharding spreads
  # both across N processes and cores. Keep this equal to the online scheduler
  # count so every connection carries one identical scheduler-local path.
  def publish_connections,
    do: Application.get_env(:ingress, :publish_connections, System.schedulers_online())

  # Gnat connection_settings (a leaf-first list of server maps) for the two
  # planes: :nats is the twitch_ingress RPC account, :nats_bus the shared BUS
  # account that carries the twitch.ingress.* firehose.
  def nats, do: Application.fetch_env!(:ingress, :nats)
  def nats_bus, do: Application.fetch_env!(:ingress, :nats_bus)
end
