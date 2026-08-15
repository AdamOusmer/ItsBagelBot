defmodule Ingress.Nats.Publisher.WireTest do
  # async: false — a wire context owns a named ETS table, and these cases drive
  # the same shard fixtures the collector suites use.
  use ExUnit.Case, async: false

  alias Ingress.Config.Publish, as: PublishConfig
  alias Ingress.Nats.CohortSender
  alias Ingress.Nats.Publisher.{AckPath, Pending, Wire}
  alias Ingress.Nats.Publisher.Wire.{Atomic, Single}

  @subject "twitch.ingress.event.standard"
  @stored ~s({"stream":"TWITCH_INGRESS","seq":7})
  @rejected ~s({"error":{"code":503,"description":"no responders"}})
  @malformed "truncated"

  # Both fake connections a wire is exercised against, told apart by one option
  # rather than by a second copy of the GenServer boilerplate:
  #
  #   * the default answers every publish and forwards it to the test process;
  #   * `stalled: true` is alive, connected, and never answers — the wedged
  #     socket write (TLS renegotiation, a full kernel send buffer, the
  #     server's slow-consumer write deadline) that Gnat's own 5s call default
  #     would sit through. It forwards nothing, so a stalled connection cannot
  #     put a message in a mailbox a later refute_receive reads.
  defmodule FakeGnat do
    use GenServer

    def start_link(opts),
      do: GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))

    @impl true
    def init(opts),
      do: {:ok, %{test: Keyword.get(opts, :test), stalled: Keyword.get(opts, :stalled, false)}}

    @impl true
    def handle_call({:pub, _topic, _message, _opts}, _from, %{stalled: true} = state),
      do: {:noreply, state}

    def handle_call({:pub, topic, message, opts}, _from, state) do
      send(state.test, {:pub, topic, message, opts})
      {:reply, :ok, state}
    end
  end

  setup do
    conn = :gnat_bus_pub_wire_test
    start_supervised!({FakeGnat, [name: conn, test: self()]})

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
      # The collector applies no PubAcks and runs no sweep ticks while a wire
      # write blocks, so the write has to give up well before the deadline the
      # next sweep measures against — otherwise one stalled socket resolves as
      # a window-wide ack timeout instead of as a definite :not_connected.
      assert PublishConfig.call_timeout_ms() < PublishConfig.ack_timeout_ms()
    end

    test "a connection that never answers surfaces as :not_connected", %{wire: wire} do
      conn = :gnat_bus_pub_wire_stalled
      # Own child id: the answering connection from setup is already supervised
      # under the module's default one.
      start_supervised!({FakeGnat, [name: conn, stalled: true]}, id: :stalled_gnat)

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
      # Bounding the call means reproducing Gnat.pub/4's private header
      # preparation. Gnat.Command.build/4 matches a literal
      # [headers: _, reply_to: _] over cowlib-encoded iodata, so a wrong
      # encoding or a wrong key order is a FunctionClauseError on the first
      # traced publish in production rather than anything a mock would catch.
      admit(wire, 2)
      Single.send_cohort([{@subject, "{}", [{"traceparent", "00-a-b-01"}], nil}], wire)
      assert_receive {:pub, @subject, traced, traced_opts}, 500
      Single.send_cohort([entry(1)], wire)
      assert_receive {:pub, @subject, plain, plain_opts}, 500

      assert [headers: _, reply_to: _] = traced_opts
      assert [reply_to: _] = plain_opts
      assert is_list(Gnat.Command.build(:pub, @subject, traced, traced_opts))
      assert is_list(Gnat.Command.build(:pub, @subject, plain, plain_opts))
    end
  end

  describe "the shipped default" do
    test "the whole fleet's open batches stay under the broker's per-stream cap" do
      # The broker caps concurrently-open atomic batches at 50 PER STREAM, and
      # every shard in the fleet opens against the same TWITCH_INGRESS. This is
      # the arithmetic that picks the number, so it is asserted rather than
      # restated in a comment: an over-cap open is answered 10210/429 and
      # re-drives per message, which removes the batching this wire exists for
      # exactly when every shard is saturated at once.
      #
      # If deploy/k8s/twitch-ingress.yaml changes `replicas` or the `+S` flag in
      # ERL_FLAGS, this test is the thing that should fail.
      broker_cap_per_stream = 50
      replicas = 3
      schedulers_per_replica = 2
      fleet_shards = replicas * schedulers_per_replica

      assert fleet_shards * PublishConfig.batch_inflight() <= broker_cap_per_stream

      # And it must not be so far under that shards bypass on ordinary latency:
      # ~1000 cohorts/s at publish_batch_wait_ms = 1 means the window covers
      # 1000/s x its commit-ack latency, so 8 buys 8 ms of R3 quorum commit.
      assert PublishConfig.batch_inflight() >= 8
      assert Application.get_env(:ingress, :publish_batch_inflight, 8) == 8
    end

    test "a swept batch's slot is held for the broker's own batch timeout" do
      # Nothing on the wire cancels an open batch; only the broker's inactivity
      # timer does, and its default is 10 s.
      assert PublishConfig.batch_hold_ms() == 10_000
      assert PublishConfig.batch_hold_ms() > PublishConfig.ack_timeout_ms()
    end

    test "cohorts ride the atomic wire unless single is asked for" do
      # The lane stream is replicated, so a per-event PubAck costs a RAFT
      # quorum round trip each; the atomic wire pays it once per cohort.
      # INGRESS_PUBLISH_WIRE=single is the compatibility escape hatch, and both
      # the env parser and the config accessor have to agree on the default.
      assert Application.get_env(:ingress, :publish_wire, :atomic) == :atomic
      assert PublishConfig.wire() == :atomic
    end
  end

  describe "the behaviour seam" do
    test "every shipped wire implements the whole contract" do
      for module <- [Single, Atomic] do
        Code.ensure_loaded!(module)
        assert Wire in module.module_info(:attributes)[:behaviour]
        assert function_exported?(module, :send_cohort, 2)
        assert function_exported?(module, :ack, 3)
        assert function_exported?(module, :expire, 2)
      end
    end

    test "a wire call touches only the shard context it is handed", %{wire: wire} do
      # No collector process exists in this suite: the wires run as plain
      # functions over the struct, which is what keeps the process model the
      # GenServer's alone.
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

      assert Pending.pending(wire.counter) == 0
      assert Pending.take_acked(wire.counter) == 1
      assert :ets.info(wire.table, :size) == 0
    end

    test "atomic settles the whole cohort on one commit ack", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(commit_ref(wire, 3), @stored, wire) == :ok

      assert Pending.pending(wire.counter) == 0
      assert Pending.take_acked(wire.counter) == 3
      assert Pending.batches_inflight(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
    end
  end

  describe "a definite rejection re-drives, because nothing was stored" do
    test "single retries the event inside the attempt budget", %{wire: wire} do
      publish_single(wire)
      ref = single_ref(wire)

      assert Single.ack(ref, @rejected, wire) == :ok

      assert_receive {:pub, @subject, _json, retry_opts}, 500
      assert Keyword.fetch!(retry_opts, :reply_to) =~ ".s."
      refute headers(retry_opts)["nats-msg-id"]

      # Still outstanding: the retry is a fresh attempt on the same row.
      assert Pending.pending(wire.counter) == 1
      assert Pending.take_retried(wire.counter) == 1
      assert Pending.take_failed(wire.counter) == 0
    end

    test "a retry that never leaves the VM settles now, not at the sweep", %{wire: wire} do
      publish_single(wire)
      ref = single_ref(wire)

      # The BUS connection flapped between the rejection and the republish, so
      # the publisher already knows the message never reached the socket.
      # Holding the row for another ack timeout would keep the shard's
      # admission window saturated and count a retry that never happened.
      offline = %{wire | conn: :gnat_bus_pub_wire_absent}

      assert Single.ack(ref, @rejected, offline) == :ok

      assert_dropped(wire, 1)
      assert Pending.take_retried(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
    end

    test "single drops once the attempt budget is spent", %{wire: wire} do
      publish_single(wire)
      {:single, id} = ref = single_ref(wire)
      Pending.insert_single(wire.table, id, @subject, "{}", wire.max_attempts)

      assert Single.ack(ref, @rejected, wire) == :ok

      assert_dropped(wire, 1)
      assert Pending.take_retried(wire.counter) == 0
    end

    test "atomic falls back to dedup-free per-message publishes", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(commit_ref(wire, 3), @rejected, wire) == :ok

      for _ <- 1..3 do
        assert_receive {:pub, @subject, _json, opts}, 500
        assert Keyword.fetch!(opts, :reply_to) =~ ".s."
        refute headers(opts)["nats-batch-id"]
        refute headers(opts)["nats-msg-id"]
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

      assert_dropped(wire, 1)
      assert Pending.take_retried(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
    end

    test "single drops a swept ack timeout without republishing", %{wire: wire} do
      publish_single(wire)
      row = swept_row(wire, 1)

      assert Single.expire(row, wire) == :ok

      assert_dropped(wire, 1)
    end

    test "atomic drops a malformed commit without falling back", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(commit_ref(wire, 3), @malformed, wire) == :ok

      assert_dropped(wire, 3)
      assert Pending.take_batch_fallback(wire.counter) == 0
      assert Pending.batches_inflight(wire.counter) == 0
    end

    test "atomic keeps a zero-byte open reply pending for its commit", %{wire: wire} do
      publish_cohort(wire, 3)

      assert Atomic.ack(start_ref(wire, 3), "", wire) == :ok

      refute_receive {:pub, _, _, _}, 100
      assert Pending.pending(wire.counter) == 3
      assert Pending.batches_inflight(wire.counter) == 1
      assert :ets.info(wire.table, :size) == 1
    end

    test "atomic drops a swept cohort whole", %{wire: wire} do
      publish_cohort(wire, 3)
      row = swept_row(wire, 3)

      assert Atomic.expire(row, wire) == :ok

      assert_dropped(wire, 3)
    end
  end

  describe "a swept batch keeps the slot the broker is still holding" do
    test "the local slot outlives the events, then retires on its own row", %{wire: wire} do
      # Nothing on the wire cancels an open atomic batch: the broker keeps its
      # per-stream in-flight slot (and the staged cohort in the leader's RAM)
      # until its own inactivity timer. Freeing the local slot at the ack
      # deadline would let this shard recycle its whole cap several times
      # through a budget the broker has not released.
      publish_cohort(wire, 3)
      {id, :batch, _entries, stamp} = row = swept_row(wire, 3)

      assert Atomic.expire(row, wire) == :ok

      assert Pending.batches_inflight(wire.counter) == 1
      # The hold carries the ORIGINAL open stamp, so it retires on the broker's
      # clock rather than restarting one at sweep time.
      assert [{^id, :batch_hold, ^stamp} = hold] = :ets.tab2list(wire.table)

      assert Atomic.expire(hold, wire) == :ok

      assert Pending.batches_inflight(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
      # The hold row carries no events, so retiring it settles nothing.
      assert Pending.take_failed(wire.counter) == 3
      assert Pending.take_acked(wire.counter) == 0
    end

    test "a late reply proves the broker is done and hands the slot back", %{wire: wire} do
      publish_cohort(wire, 3)
      ref = commit_ref(wire, 3)
      [row] = :ets.tab2list(wire.table)
      Atomic.expire(row, wire)
      assert Pending.batches_inflight(wire.counter) == 1

      assert Atomic.ack(ref, @stored, wire) == :ok

      assert Pending.batches_inflight(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
      # The events were already dropped by the dedup-free rule and stay dropped.
      assert Pending.take_acked(wire.counter) == 0
      assert Pending.take_failed(wire.counter) == 3
    end
  end

  describe "a broker that never read the batch headers" do
    test "a success PubAck on the open inbox resolves the cohort as stored", %{wire: wire} do
      # Only a broker that ignored Nats-Batch-* answers the opening message
      # with a stored PubAck (a 2.14 broker sends a zero-byte ack when staging
      # and an error PubAck for every rejection, allow_atomic being off
      # included). The cohort is on the stream, so counting it failed would
      # report total data loss during healthy ingest.
      publish_cohort(wire, 3)

      assert Atomic.ack(start_ref(wire, 3), @stored, wire) == :ok

      refute_receive {:pub, _, _, _}, 100
      assert Pending.pending(wire.counter) == 0
      assert Pending.take_acked(wire.counter) == 3
      assert Pending.take_failed(wire.counter) == 0
      assert Pending.take_batch_fallback(wire.counter) == 0
      assert Pending.batches_inflight(wire.counter) == 0
      assert :ets.info(wire.table, :size) == 0
    end

    test "the outcome is counted apart from every failure counter", %{wire: wire} do
      publish_cohort(wire, 2)

      Atomic.ack(start_ref(wire, 2), @stored, wire)

      assert Pending.take_batch_headers_ignored(wire.counter) == 1
      assert Pending.take_batch_headers_ignored(wire.counter) == 0
    end
  end

  ## Fixtures

  # Mirrors what scheduler-local admission does before an event is queued, so
  # the in-flight window is charged the way a wire expects to find it.
  defp admit(wire, count), do: Enum.each(1..count, fn _ -> Pending.reserve(wire.counter) end)

  defp entry(n), do: {@subject, ~s({"n":#{n}}), nil}

  # The two steps every case opens with: charge admission for the cohort, then
  # hand it to a wire. Named per wire because which one wrote the cohort is the
  # whole point of the case that follows.
  defp publish_single(wire), do: publish(Single, wire, 1)
  defp publish_cohort(wire, count), do: publish(Atomic, wire, count)

  defp publish(wire_impl, wire, count) do
    admit(wire, count)
    wire_impl.send_cohort(Enum.map(1..count, &entry/1), wire)
  end

  # The pending row the sweep would find. The cohort's own writes are drained
  # first so a later refute_receive can only trip on a re-drive.
  defp swept_row(wire, count) do
    Enum.each(1..count, fn _ -> drain_reply() end)
    [row] = :ets.tab2list(wire.table)
    row
  end

  # Every dedup-free drop resolves the same way, whichever wire and whichever
  # ambiguous outcome produced it: nothing re-driven, no pending work left, and
  # every event in the cohort counted failed.
  defp assert_dropped(wire, count) do
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

  # Drains the whole cohort before picking a reply, so the leftovers cannot be
  # mistaken for a later fallback publish.
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

  # Gnat preps headers into cowlib iodata before the connection call.
  defp headers(opts) do
    for [key, ": ", value, "\r\n"] <- Keyword.get(opts, :headers, []), into: %{} do
      {String.downcase(key), IO.iodata_to_binary(value)}
    end
  end
end
