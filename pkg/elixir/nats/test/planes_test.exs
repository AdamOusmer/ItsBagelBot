# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.PlanesTest do
  use ExUnit.Case, async: false

  alias Platform.Nats
  alias Platform.Nats.{Planes, Settings}
  alias Platform.Nats.TestSupport.NatsServer

  @moduletag :tmp_dir

  setup %{tmp_dir: dir} do
    NatsServer.start_planes(dir)
  end

  test "request reaches the responder on the separate leaf broker", %{rpc: endpoint} do
    peer = :leaf_responder
    settings = Settings.connection(endpoint, %{}, nil)
    start_supervised!(Nats.connection_child(peer, peer, [settings]))
    NatsServer.await_connection(peer)

    subject = "test.leaf.rpc"
    {:ok, _sid} = Nats.subscribe(peer, self(), subject)
    :ok = Nats.publish(peer, subject, "ready")
    assert_receive {:msg, %{topic: ^subject, body: "ready"}}, 1_000

    request = Task.async(fn -> Planes.request({subject, "request", [receive_timeout: 1_000]}) end)
    assert_receive {:msg, %{topic: ^subject, body: "request", reply_to: reply}}, 1_000
    :ok = Nats.publish(peer, reply, "leaf response")
    assert {:ok, %{body: "leaf response"}} = Task.await(request)
  end

  test "JetStream API reaches the hub and the leaf has no JetStream responders" do
    message = {"$JS.API.INFO", "", [receive_timeout: 1_000]}

    assert {:error, :no_responders} = Planes.request(message)
    assert {:ok, %{body: body}} = Planes.jetstream_request(message)

    assert %{"type" => "io.nats.jetstream.api.v1.account_info_response", "streams" => 0} =
             Jason.decode!(body)
  end

  test "a queue group delivers each publication to one subscriber" do
    subject = "test.queue"
    parent = self()

    for member <- 1..2 do
      pid =
        spawn_link(fn ->
          receive do
            {:msg, _message} -> send(parent, {:received, member})
          end
        end)

      {:ok, _sid} = Planes.subscribe(:jetstream, pid, subject, queue_group: "workers")
    end

    :ok = Planes.publish(:jetstream, subject, "once")
    assert_receive {:received, _member}, 1_000
    refute_receive {:received, _member}, 300
  end

  test "core publications and subscriptions stay on the named plane" do
    subject = "test.core"

    for plane <- [:rpc, :jetstream] do
      {:ok, _sid} = Planes.subscribe(plane, self(), subject)
    end

    for plane <- [:rpc, :jetstream] do
      %{pid: pid} = Planes.connection(plane)
      body = Atom.to_string(plane)

      :ok = Planes.publish(plane, subject, body)
      assert_receive {:msg, %{gnat: ^pid, topic: ^subject, body: ^body}}, 1_000
    end

    refute_receive {:msg, %{topic: ^subject}}
  end
end
