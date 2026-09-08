# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.PublisherAtMostOnceTest do
  # async: false — the publisher uses a named process, a named ETS table and a
  # global persistent_term context, so it cannot share the VM with a parallel
  # instance of itself.
  use Ingress.PublisherCase, async: false

  setup context do
    conn = :gnat_bus_pub_at_most_once_test

    put_env(
      [publish_batch_size: 8, publish_batch_wait_ms: 10] ++ Map.get(context, :overrides, [])
    )

    start_fake_gnat(conn)
    start_publisher(conn)
  end

  test "the publisher never emits or stores a Nats-Msg-Id", %{ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, opts}, 500

    refute Map.has_key?(headers_map(opts), "nats-msg-id")

    # The pending-row shape contains no dormant dedup-id slot.
    assert [{_id, :single, _subject, _json, 1, _ts}] = :ets.tab2list(ctx.table)
  end

  test "an ambiguous ack timeout drops instead of retrying", %{publisher: publisher, ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, _opts}, 500

    age_pending_rows(ctx)
    send(publisher, :sweep)
    _state = :sys.get_state(publisher)

    # Dropped, not re-published.
    refute_receive {:pub, _, _, _}, 100
    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 1) == 0
    # Counter 5 is failed, counter 4 is retried.
    assert :atomics.get(ctx.counter, 5) == 1
    assert :atomics.get(ctx.counter, 4) == 0
  end

  test "a definite error PubAck still retries without dedup", %{publisher: publisher, ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, opts}, 500
    reply = Keyword.fetch!(opts, :reply_to)

    # An error PubAck means the broker did not store the event; the retry
    # cannot double-store even without a dedup id.
    send(
      publisher,
      {:msg, %{topic: reply, body: ~s({"error":{"code":503,"description":"no responders"}})}}
    )

    assert_receive {:pub, _topic, _json, retry_opts}, 500
    refute Map.has_key?(headers_map(retry_opts), "nats-msg-id")

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, 4) == 1
    assert :atomics.get(ctx.counter, 1) == 1
  end

  test "a malformed single PubAck drops instead of retrying", %{publisher: publisher, ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, opts}, 500

    send(publisher, {:msg, %{topic: Keyword.fetch!(opts, :reply_to), body: "truncated"}})
    _state = :sys.get_state(publisher)

    refute_receive {:pub, _, _, _}, 100
    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 1) == 0
    assert :atomics.get(ctx.counter, 5) == 1
    assert :atomics.get(ctx.counter, 4) == 0
  end

  @tag overrides: [publish_wire: :atomic, publish_batch_size: 3]
  test "an expired unprotected atomic batch is dropped whole", %{
    publisher: publisher,
    ctx: ctx
  } do
    for n <- 1..3 do
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":#{n}})) == :ok
    end

    for _ <- 1..3, do: assert_receive({:pub, _, _, _}, 500)
    assert [{_id, :batch, _entries, _ts}] = :ets.tab2list(ctx.table)

    age_pending_rows(ctx)
    send(publisher, :sweep)
    _state = :sys.get_state(publisher)

    # No per-message re-drive: the commit may have landed, so an unprotected
    # cohort must not be re-published.
    refute_receive {:pub, _, _, _}, 100
    assert :atomics.get(ctx.counter, 1) == 0
    assert :atomics.get(ctx.counter, 5) == 3

    # The events are resolved, but the broker still holds the abandoned batch,
    # so the shard keeps owing it one in-flight slot until the broker's own
    # batch timeout — represented by an event-free hold row.
    assert [{_id, :batch_hold, _ts}] = :ets.tab2list(ctx.table)
    assert :atomics.get(ctx.counter, 7) == 1

    age_pending_rows(ctx)
    send(publisher, :sweep)
    _state = :sys.get_state(publisher)

    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 7) == 0
  end

  @tag overrides: [publish_wire: :atomic, publish_batch_size: 3]
  test "a malformed atomic commit PubAck drops the cohort without fallback", %{
    publisher: publisher,
    ctx: ctx
  } do
    for n <- 1..3 do
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":#{n}})) == :ok
    end

    publishes =
      for _ <- 1..3 do
        assert_receive({:pub, _, _, opts}, 500)
        opts
      end

    commit =
      Enum.find_value(publishes, fn opts ->
        case Keyword.get(opts, :reply_to) do
          nil -> nil
          reply -> if String.contains?(reply, ".bc."), do: reply
        end
      end)

    assert is_binary(commit)
    send(publisher, {:msg, %{topic: commit, body: "truncated"}})
    _state = :sys.get_state(publisher)

    refute_receive {:pub, _, _, _}, 100
    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 1) == 0
    assert :atomics.get(ctx.counter, 5) == 3
    assert :atomics.get(ctx.counter, 7) == 0
  end
end
