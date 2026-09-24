# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher.WireTest do
  use Ingress.PublisherCase, async: false

  alias Ingress.Config.Publish, as: PublishConfig
  alias Ingress.Nats.CohortSender
  alias Ingress.Nats.Publisher.{AckPath, Pending, Wire}
  alias Ingress.Nats.Publisher.Wire.{Atomic, Single}

  @subject "twitch.ingress.event.standard"
  @stored ~s({"stream":"TWITCH_INGRESS","seq":7})
  @rejected ~s({"error":{"code":503,"description":"no responders"}})
  @malformed "truncated"

  setup do
    conn = :gnat_bus_pub_wire_test
    start_fake_gnat(conn)

    {token, prefix} = AckPath.new_inbox()
    senders = CohortSender.start(2)
    on_exit(fn -> CohortSender.stop(senders) end)

    wire = %Wire{
      conn: conn,
      table: Pending.new_table(:wire_seam),
      counter: Pending.new_counter(),
      prefix: prefix,
      batch_token: token,
      senders: senders,
      max_attempts: 3,
      call_timeout_ms: 200
    }

    %{wire: wire}
  end

  describe "classify/1" do
    test "a stored PubAck is a success" do
      assert Wire.classify(@stored) == :ok
    end

    test "a duplicate PubAck still counts as stored" do
      assert Wire.classify(~s({"stream":"TWITCH_INGRESS","seq":7,"duplicate":true})) == :ok
    end

    test "an error PubAck is the only definite negative" do
      assert Wire.classify(@rejected) == :rejected
    end

    test "an unreadable reply is ambiguous, never a rejection" do
      assert Wire.classify(@malformed) == :ambiguous
      assert Wire.classify("") == :ambiguous
      assert Wire.classify(~s({"unexpected":true})) == :ambiguous
    end
  end

  describe "a blocking wire write is bounded" do
    test "the call timeout sits below the ack budget it would otherwise blow" do
      assert PublishConfig.call_timeout_ms() < PublishConfig.ack_timeout_ms()
    end

    test "a connection that never answers surfaces as :not_connected", %{wire: wire} do
      conn = :gnat_bus_pub_wire_stalled
      start_fake_gnat(conn, mode: :stalled, id: :stalled_gnat)

      started = System.monotonic_time(:millisecond)
      opts = [reply_to: AckPath.single(wire.prefix, 1), headers: [{"traceparent", "00-a-b-01"}]]

      assert Wire.safe_pub(conn, {@subject, "{}", opts}, 150) == {:error, :not_connected}

      assert System.monotonic_time(:millisecond) - started < 1_000
    end

    test "an absent connection is :not_connected without waiting at all" do
      assert Wire.safe_pub(:gnat_bus_pub_wire_absent, {@subject, "{}", []}, 5_000) ==
               {:error, :not_connected}
    end

    test "prepared opts are still the shape Gnat's own command builder accepts", %{wire: wire} do
      admit(wire, 2)
      Single.send_cohort([{@subject, "{}", [{"traceparent", "00-a-b-01"}], nil}], wire)
      assert_receive {:pub, @subject, traced, traced_opts}, 500
      Single.send_cohort([entry(1)], wire)
      assert_receive {:pub, @subject, plain, plain_opts}, 500

      assert [headers: _, reply_to: _] = traced_opts
      assert [reply_to: _] = plain_opts

      for {json, opts} <- [{traced, traced_opts}, {plain, plain_opts}] do
        assert is_list(Gnat.Command.build(:pub, @subject, json, opts))
      end
    end
  end

  describe "the shipped default" do
    test "the whole fleet's open batches stay under the broker's per-stream cap" do
      broker_cap_per_stream = 50
      replicas = 3
      schedulers_per_replica = 2
      fleet_shards = replicas * schedulers_per_replica

      assert fleet_shards * PublishConfig.batch_inflight() <= broker_cap_per_stream

      assert PublishConfig.batch_inflight() >= 8
      assert Application.get_env(:ingress, :publish_batch_inflight, 8) == 8
    end

    test "a swept batch's slot is held for the broker's own batch timeout" do
      assert PublishConfig.batch_hold_ms() == 10_000
      assert PublishConfig.batch_hold_ms() > PublishConfig.ack_timeout_ms()
    end

    test "cohorts ride the atomic wire unless single is asked for" do
      assert Application.get_env(:ingress, :publish_wire, :atomic) == :atomic
      assert PublishConfig.wire() == :atomic
    end
  end

  describe "the behaviour seam" do
    test "every shipped wire implements the whole contract" do
      for module <- [Single, Atomic] do
        Code.ensure_loaded!(module)
        assert Wire in module.module_info(:attributes)[:behaviour]

        for {fun, arity} <- [send_cohort: 2, ack: 3, expire: 2] do
          assert function_exported?(module, fun, arity)
        end
      end
    end

    test "a wire call touches only the shard context it is handed", %{wire: wire} do
      refute Process.whereis(Ingress.Nats.Publisher.process_name(:wire_seam))

      admit(wire, 1)
      assert Single.send_cohort([entry(1)], wire) == :ok
      assert_receive {:pub, @subject, _json, _opts}, 500
      assert :ets.info(wire.table, :size) == 1
    end
  end

  describe "a stored PubAck resolves the pending work" do
    test "single settles the one event", %{wire: wire} do
      publish_single(wire)

      assert Single.ack(single_ref(wire), @stored, wire) == :ok

      assert counter_ledger(wire) == %{
               pending: 0,
               acked: 1,
               failed: 0,
               retried: 0,
               fallback: 0,
               inflight: 0,
               rows: 0
             }
    end

    test "atomic settles the whole cohort on one commit ack", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(commit_ref(wire, 3), @stored, wire) == :ok

      assert counter_ledger(wire) == %{
               pending: 0,
               acked: 3,
               failed: 0,
               retried: 0,
               fallback: 0,
               inflight: 0,
               rows: 0
             }
    end
  end

  describe "a definite rejection re-drives, because nothing was stored" do
    test "single retries the event inside the attempt budget", %{wire: wire} do
      publish_single(wire)
      ref = single_ref(wire)

      assert Single.ack(ref, @rejected, wire) == :ok

      assert_receive {:pub, @subject, _json, retry_opts}, 500
      require_dedup_free_single(retry_opts)

      assert counter_ledger(wire) == %{
               pending: 1,
               acked: 0,
               failed: 0,
               retried: 1,
               fallback: 0,
               inflight: 0,
               rows: 1
             }
    end

    test "a retry that never leaves the VM settles now, not at the sweep", %{wire: wire} do
      publish_single(wire)
      ref = single_ref(wire)

      offline = %{wire | conn: :gnat_bus_pub_wire_absent}

      assert Single.ack(ref, @rejected, offline) == :ok

      require_dropped(wire, 1)
      assert Pending.take_retried(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
    end

    test "single drops once the attempt budget is spent", %{wire: wire} do
      publish_single(wire)
      {:single, id} = ref = single_ref(wire)
      Pending.insert_single(wire.table, id, @subject, "{}", wire.max_attempts)

      assert Single.ack(ref, @rejected, wire) == :ok

      require_dropped(wire, 1)
      assert Pending.take_retried(wire.counter) == 0
    end

    test "atomic falls back to dedup-free per-message publishes", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(commit_ref(wire, 3), @rejected, wire) == :ok

      for _ <- 1..3 do
        assert_receive {:pub, @subject, _json, opts}, 500
        require_dedup_free_single(opts)
      end

      assert Pending.pending(wire.counter) == 3
      assert Pending.take_batch_fallback(wire.counter) == 1
      assert Pending.batches_inflight(wire.counter) == 0
    end

    test "atomic falls back on a rejected open without waiting for the commit", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(start_ref(wire, 3), @rejected, wire) == :ok

      for _ <- 1..3, do: assert_receive({:pub, @subject, _json, _opts}, 500)
      assert Pending.take_batch_fallback(wire.counter) == 1
    end
  end

  describe "an ambiguous outcome drops, because a replay could double-store" do
    test "single drops a malformed PubAck", %{wire: wire} do
      publish_single(wire)

      assert Single.ack(single_ref(wire), @malformed, wire) == :ok

      require_dropped(wire, 1)
      assert Pending.take_retried(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
    end

    test "single drops a swept ack timeout without republishing", %{wire: wire} do
      publish_single(wire)
      row = swept_row(wire, 1)

      assert Single.expire(row, wire) == :ok

      require_dropped(wire, 1)
    end

    test "atomic drops a malformed commit without falling back", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(commit_ref(wire, 3), @malformed, wire) == :ok

      require_dropped(wire, 3)
      assert Pending.take_batch_fallback(wire.counter) == 0
      assert Pending.batches_inflight(wire.counter) == 0
    end

    test "atomic keeps a zero-byte open reply pending for its commit", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(start_ref(wire, 3), "", wire) == :ok

      refute_receive {:pub, _, _, _}, 100

      assert counter_ledger(wire) == %{
               pending: 3,
               acked: 0,
               failed: 0,
               retried: 0,
               fallback: 0,
               inflight: 1,
               rows: 1
             }
    end

    test "atomic drops a swept cohort whole", %{wire: wire} do
      publish_cohort(wire, 3)
      row = swept_row(wire, 3)

      assert Atomic.expire(row, wire) == :ok

      require_dropped(wire, 3)
    end
  end

  describe "a swept batch keeps the slot the broker is still holding" do
    test "the local slot outlives the events, then retires on its own row", %{wire: wire} do
      publish_cohort(wire, 3)
      {id, :batch, _entries, stamp} = row = swept_row(wire, 3)

      assert Atomic.expire(row, wire) == :ok
      hold = require_batch_hold(wire, id, stamp)

      assert Atomic.expire(hold, wire) == :ok

      assert counter_ledger(wire) == %{
               pending: 0,
               acked: 0,
               failed: 3,
               retried: 0,
               fallback: 0,
               inflight: 0,
               rows: 0
             }
    end

    test "a late reply proves the broker is done and hands the slot back", %{wire: wire} do
      publish_cohort(wire, 3)
      ref = commit_ref(wire, 3)
      [row] = :ets.tab2list(wire.table)
      Atomic.expire(row, wire)
      assert Pending.batches_inflight(wire.counter) == 1

      assert Atomic.ack(ref, @stored, wire) == :ok

      assert counter_ledger(wire) == %{
               pending: 0,
               acked: 0,
               failed: 3,
               retried: 0,
               fallback: 0,
               inflight: 0,
               rows: 0
             }
    end
  end

  describe "a broker that never read the batch headers" do
    test "a success PubAck on the open inbox resolves the cohort as stored", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(start_ref(wire, 3), @stored, wire) == :ok

      refute_receive {:pub, _, _, _}, 100

      assert counter_ledger(wire) == %{
               pending: 0,
               acked: 3,
               failed: 0,
               retried: 0,
               fallback: 0,
               inflight: 0,
               rows: 0
             }
    end

    test "the outcome is counted apart from every failure counter", %{wire: wire} do
      publish_cohort(wire, 2)

      Atomic.ack(start_ref(wire, 2), @stored, wire)

      assert Pending.take_batch_headers_ignored(wire.counter) == 1
      assert Pending.take_batch_headers_ignored(wire.counter) == 0
    end
  end

  defp admit(wire, count), do: Enum.each(1..count, fn _ -> Pending.reserve(wire.counter) end)

  defp entry(n), do: {@subject, ~s({"n":#{n}}), nil}

  defp publish_single(wire), do: publish(Single, wire, 1)
  defp publish_cohort(wire, count), do: publish(Atomic, wire, count)

  defp publish(wire_impl, wire, count) do
    admit(wire, count)
    wire_impl.send_cohort(Enum.map(1..count, &entry/1), wire)
  end

  defp swept_row(wire, count) do
    Enum.each(1..count, fn _ -> drain_reply() end)
    [row] = :ets.tab2list(wire.table)
    row
  end

  defp counter_ledger(wire) do
    %{
      pending: Pending.pending(wire.counter),
      acked: Pending.take_acked(wire.counter),
      failed: Pending.take_failed(wire.counter),
      retried: Pending.take_retried(wire.counter),
      fallback: Pending.take_batch_fallback(wire.counter),
      inflight: Pending.batches_inflight(wire.counter),
      rows: :ets.info(wire.table, :size)
    }
  end

  defp require_batch_hold(wire, id, stamp) do
    assert Pending.batches_inflight(wire.counter) == 1
    assert [{^id, :batch_hold, ^stamp} = hold] = :ets.tab2list(wire.table)
    hold
  end

  defp require_dedup_free_single(opts) do
    assert Keyword.fetch!(opts, :reply_to) =~ ".s."
    refute headers_map(opts)["nats-batch-id"]
    refute headers_map(opts)["nats-msg-id"]
  end

  defp require_dropped(wire, count) do
    refute_receive {:pub, _, _, _}, 100
    assert Pending.pending(wire.counter) == 0
    assert Pending.take_failed(wire.counter) == count
  end

  defp single_ref(wire) do
    assert_receive {:pub, @subject, _json, opts}, 500
    AckPath.parse(Keyword.fetch!(opts, :reply_to), wire.prefix)
  end

  defp start_ref(wire, count), do: batch_ref(wire, count, ".bs.")
  defp commit_ref(wire, count), do: batch_ref(wire, count, ".bc.")

  defp batch_ref(wire, count, tag) do
    reply =
      1..count
      |> Enum.map(fn _ -> drain_reply() end)
      |> Enum.find(&tagged?(&1, tag))

    assert is_binary(reply)
    AckPath.parse(reply, wire.prefix)
  end

  defp drain_reply do
    assert_receive {:pub, @subject, _json, opts}, 500
    Keyword.get(opts, :reply_to)
  end

  defp tagged?(nil, _tag), do: false
  defp tagged?(reply, tag), do: String.contains?(reply, tag)
end
