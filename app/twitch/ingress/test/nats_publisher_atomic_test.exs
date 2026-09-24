# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.PublisherAtomicTest do
  use Ingress.PublisherCase, async: false

  @idx_pending 1
  @idx_batch_inflight 7
  @idx_batch_bypass 9

  setup do
    conn = :gnat_bus_pub_atomic_test

    put_env(
      publish_wire: :atomic,
      publish_batch_size: 3,
      publish_batch_wait_ms: 50,
      publish_batch_inflight: 4
    )

    start_fake_gnat(conn)
    start_publisher(conn)
  end

  defp enqueue_cohort do
    for n <- 1..3 do
      assert Publisher.enqueue(
               "twitch.ingress.event.standard",
               ~s({"n":#{n}})
             ) == :ok
    end
  end

  defp collect_cohort do
    for _ <- 1..3 do
      assert_receive {:pub, _topic, _json, opts}, 500
      opts
    end
  end

  test "lanes on different streams never share one atomic batch", %{publisher: publisher} do
    for subject <- ["twitch.ingress.event.standard", "twitch.ingress.event.premium"],
        n <- 1..2 do
      assert Publisher.enqueue(subject, ~s({"n":#{n}})) == :ok
    end

    by_subject =
      Enum.group_by(
        for _ <- 1..4 do
          assert_receive {:pub, topic, _json, opts}, 500
          {topic, headers_map(opts)}
        end,
        &elem(&1, 0),
        &elem(&1, 1)
      )

    assert map_size(by_subject) == 2

    batch_ids =
      for {_subject, [first, last]} <- by_subject do
        batch_id = first["nats-batch-id"]

        assert first["nats-batch-sequence"] == "1"
        assert last["nats-batch-sequence"] == "2"
        assert last["nats-batch-id"] == batch_id
        refute Map.has_key?(first, "nats-batch-commit")
        assert last["nats-batch-commit"] == "1"

        batch_id
      end

    assert length(Enum.uniq(batch_ids)) == 2

    _state = :sys.get_state(publisher)
  end

  test "a cohort travels as one sequenced atomic batch with a single commit reply", %{
    publisher: publisher,
    ctx: ctx
  } do
    enqueue_cohort()
    [first, middle, last] = collect_cohort()

    [first_headers, middle_headers, last_headers] =
      Enum.map([first, middle, last], &headers_map/1)

    batch_id = first_headers["nats-batch-id"]
    assert is_binary(batch_id) and byte_size(batch_id) <= 64
    assert middle_headers["nats-batch-id"] == batch_id
    assert last_headers["nats-batch-id"] == batch_id

    assert first_headers["nats-batch-sequence"] == "1"
    assert middle_headers["nats-batch-sequence"] == "2"
    assert last_headers["nats-batch-sequence"] == "3"

    refute Map.has_key?(first_headers, "nats-batch-commit")
    refute Map.has_key?(middle_headers, "nats-batch-commit")
    assert last_headers["nats-batch-commit"] == "1"

    refute Map.has_key?(first_headers, "nats-msg-id")
    refute Map.has_key?(middle_headers, "nats-msg-id")
    refute Map.has_key?(last_headers, "nats-msg-id")

    start_reply = Keyword.fetch!(first, :reply_to)
    commit_reply = Keyword.fetch!(last, :reply_to)
    assert String.contains?(start_reply, ".bs.")
    assert String.contains?(commit_reply, ".bc.")
    refute Keyword.has_key?(middle, :reply_to)

    send(publisher, {:msg, %{topic: start_reply, body: ""}})

    send(
      publisher,
      {:msg,
       %{
         topic: commit_reply,
         body: ~s({"stream":"TWITCH_INGRESS","seq":7,"batch":"#{batch_id}","count":3})
       }}
    )

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, @idx_pending) == 0
    assert :ets.info(ctx.table, :size) == 0
  end

  test "a definitely rejected commit falls back to dedup-free per-message publishes", %{
    publisher: publisher,
    ctx: ctx
  } do
    enqueue_cohort()
    [_first, _middle, last] = collect_cohort()

    commit_reply = Keyword.fetch!(last, :reply_to)

    send(
      publisher,
      {:msg,
       %{
         topic: commit_reply,
         body: ~s({"error":{"code":400,"err_code":10176,"description":"batch incomplete"}})
       }}
    )

    fallback = collect_cohort()

    Enum.each(fallback, fn opts ->
      headers = headers_map(opts)
      refute Map.has_key?(headers, "nats-batch-id")
      refute Map.has_key?(headers, "nats-batch-commit")
      refute Map.has_key?(headers, "nats-msg-id")
      assert Keyword.fetch!(opts, :reply_to) =~ ".s."
    end)

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, @idx_pending) == 3
    assert :ets.info(ctx.table, :size) == 3

    fallback
    |> Enum.map(&Keyword.fetch!(&1, :reply_to))
    |> Enum.with_index(1)
    |> Enum.each(fn {reply, sequence} ->
      send(
        publisher,
        {:msg, %{topic: reply, body: ~s({"stream":"TWITCH_INGRESS","seq":#{sequence}})}}
      )
    end)

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, @idx_pending) == 0
    assert :ets.info(ctx.table, :size) == 0
  end

  test "a start rejection falls back without waiting for the commit reply", %{
    publisher: publisher,
    ctx: ctx
  } do
    enqueue_cohort()
    [first, _middle, _last] = collect_cohort()

    send(
      publisher,
      {:msg,
       %{
         topic: Keyword.fetch!(first, :reply_to),
         body:
           ~s({"error":{"code":400,"err_code":10174,"description":"batch publish not enabled"}})
       }}
    )

    fallback = collect_cohort()
    assert length(fallback) == 3

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, @idx_pending) == 3
  end

  test "cohorts past the in-flight batch budget degrade to individual publishes", %{
    ctx: ctx
  } do
    :atomics.put(ctx.counter, @idx_batch_inflight, 4)

    enqueue_cohort()
    cohort = collect_cohort()

    Enum.each(cohort, fn opts ->
      headers = headers_map(opts)
      refute Map.has_key?(headers, "nats-batch-id")
      assert Keyword.fetch!(opts, :reply_to) =~ ".s."
    end)

    assert :atomics.get(ctx.counter, @idx_batch_bypass) == 1

    :atomics.put(ctx.counter, @idx_batch_inflight, 0)
  end
end
