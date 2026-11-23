# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.AckTest do
  use ExUnit.Case, async: false

  alias Platform.Nats.Ack
  alias Platform.Nats.TestSupport.{Jetstream, NatsServer}

  @moduletag :tmp_dir
  @consumers [{"S", "d"}, {"T", "e"}]

  setup %{tmp_dir: dir} do
    NatsServer.start_planes(dir)
    :ok = Jetstream.create_stream!("S", ["s.>"])
    :ok = Jetstream.create_consumer!("S", "d", %{ack_wait: 500_000_000})
    :ok = Jetstream.publish!("s.one", "payload")
    {:ok, %{reply_to: ack}} = Jetstream.next("S", "d", 1_000)
    %{ack: ack}
  end

  test "ack and term settle the message", %{ack: ack} do
    for settle <- [:ack, :term] do
      Jetstream.publish!("s.#{settle}", "again")
      {:ok, %{reply_to: pending}} = Jetstream.next("S", "d", 1_000)
      assert :ok = apply(Ack, settle, [:jetstream, pending])
    end

    assert :ok = Ack.ack(:jetstream, ack)

    assert Jetstream.eventually(fn ->
             Jetstream.consumer_info!("S", "d")["num_ack_pending"] == 0
           end)
  end

  test "nak requeues at once and nak with delay waits", %{ack: ack} do
    assert :ok = Ack.nak(:jetstream, ack)
    {:ok, %{reply_to: again}} = Jetstream.next("S", "d", 1_000)
    assert :ok = Ack.nak(:jetstream, again, 700_000_000)
    assert {:ok, %{status: "408"}} = Jetstream.next("S", "d", 200)
    assert {:ok, %{body: "payload"}} = Jetstream.next("S", "d", 2_000)
  end

  test "progress extends the ack wait", %{ack: ack} do
    Process.sleep(300)
    assert :ok = Ack.progress(:jetstream, ack)
    Process.sleep(300)
    assert {:ok, %{status: "408"}} = Jetstream.next("S", "d", 100)
  end

  @stream_forms [
    {"$JS.ACK.S.d.1.2.3.4.5", true},
    {"$JS.ACK.dom.hash.S.d.1.2.3.4.5.rand", true},
    {"$JS.ACK.T.e.1.2.3.4.5", true},
    {"$JS.ACK.S.other.1.2.3.4.5", false},
    {"$JS.ACK.S.d.1.2.3.4.x", false},
    {"$JS.ACK.S.d.1.2.3.4", false},
    {"$JS.ACK.dom.hash.S.d.1.2.3.4.5.ra.nd", false},
    {"$JS.ACK.dom.has.h.S.d.1.2.3.4.5", false},
    {"$JS.ACK.dom.ha sh.S.d.1.2.3.4.5.rand", false},
    {"other.S.d.1.2.3.4.5", false}
  ]

  test "subject? validates both ack subject forms against the consumers" do
    for {subject, valid?} <- @stream_forms do
      assert Ack.subject?(subject, @consumers) == valid?, subject
    end
  end
end
