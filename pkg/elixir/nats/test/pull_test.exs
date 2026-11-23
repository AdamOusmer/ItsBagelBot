# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.PullTest do
  use ExUnit.Case, async: false

  alias Platform.Nats.{Ack, Planes, Pull}
  alias Platform.Nats.TestSupport.{Jetstream, NatsServer}

  @moduletag :tmp_dir

  setup %{tmp_dir: dir} do
    NatsServer.start_planes(dir)
    :ok = Jetstream.create_stream!("P", ["p.>"])
    :ok = Jetstream.create_consumer!("P", "d")
    start_supervised!({Task.Supervisor, name: :pull_tasks})
    test = self()

    handler = fn message ->
      send(test, {:handled, self(), message.body})
      Ack.ack(:jetstream, message.reply_to)
    end

    pull =
      start_supervised!(
        {Pull,
         name: :pull_under_test,
         stream: "P",
         durable: "d",
         handler: handler,
         task_supervisor: :pull_tasks,
         batch: 2,
         expires_ns: 1_000_000_000,
         inbox_prefix: "_INBOX.pull"}
      )

    assert Jetstream.eventually(fn -> Pull.attached?(pull) end)
    %{pull: pull}
  end

  test "every published message reaches the handler once", %{pull: pull} do
    for n <- 1..5, do: Jetstream.publish!("p.msg", "m#{n}")

    for n <- 1..5 do
      body = "m#{n}"
      assert_receive {:handled, _task, ^body}, 2_000
    end

    assert :ok = Pull.await_idle(pull, 1_000)

    assert Jetstream.eventually(fn ->
             Jetstream.consumer_info!("P", "d")["num_ack_pending"] == 0
           end)
  end

  test "await_idle waits for in-flight handlers" do
    Jetstream.create_consumer!("P", "blocked")

    blocked =
      start_extra(:blocked, "blocked", fn message -> receive do: (:release -> message) end)

    Jetstream.publish!("p.block", "held")
    assert Jetstream.eventually(fn -> task_running?(:blocked_tasks) end)
    assert {:error, :timeout} = Pull.await_idle(blocked, 100)

    for {_id, pid, _type, _mods} <- Supervisor.which_children(:blocked_tasks) do
      send(pid, :release)
    end

    assert :ok = Pull.await_idle(blocked, 2_000)
  end

  test "a crashing handler still releases await_idle" do
    test = self()

    Jetstream.create_consumer!("P", "crashing")

    crashing =
      start_extra(:crashing, "crashing", fn _message ->
        send(test, :crashing)
        exit(:boom)
      end)

    ExUnit.CaptureLog.capture_log(fn ->
      Jetstream.publish!("p.crash", "boom")
      assert_receive :crashing, 2_000
      assert :ok = Pull.await_idle(crashing, 2_000)
    end)
  end

  test "attached? turns true well before the pull expires on an empty consumer" do
    Jetstream.create_consumer!("P", "quick")
    quick = start_extra(:quick, "quick", fn message -> message end)
    assert Jetstream.eventually(fn -> Pull.attached?(quick) end, 20)
  end

  test "pull recovers quickly when the consumer is deleted and recreated" do
    test = self()
    Jetstream.create_stream!("F", ["f.>"])
    Jetstream.create_consumer!("F", "flip")

    flip =
      start_extra(
        :flip,
        "flip",
        fn message ->
          send(test, {:flip, message.body})
          Ack.ack(:jetstream, message.reply_to)
        end,
        stream: "F",
        expires_ns: 1_500_000_000
      )

    assert Jetstream.eventually(fn -> Pull.attached?(flip) end, 20)
    Jetstream.api!("$JS.API.CONSUMER.DELETE.F.flip", %{})
    Jetstream.create_consumer!("F", "flip")
    Jetstream.publish!("f.flip", "again")
    assert_receive {:flip, "again"}, 4_000
  end

  test "an error status re-attaches through the consumer check" do
    quick = start_extra(:statused, "d", fn message -> message end)
    assert Jetstream.eventually(fn -> Pull.attached?(quick) end, 20)
    %{sid: sid} = :sys.get_state(quick)
    send(quick, {:msg, %{sid: sid, status: "409"}})
    refute Pull.attached?(quick)
    assert Jetstream.eventually(fn -> Pull.attached?(quick) end, 40)
  end

  test "a status followed by a connection loss attaches once" do
    test = self()
    id = {:attach_once, make_ref()}
    Jetstream.create_consumer!("P", "once")
    once = start_extra(:once, "once", fn message -> message end)
    assert Jetstream.eventually(fn -> Pull.attached?(once) end, 20)

    :telemetry.attach(
      id,
      [:platform, :nats, :pull, :attach],
      fn _event, _measurements, %{name: name}, _config -> send(test, {:attach, name}) end,
      nil
    )

    on_exit(fn -> :telemetry.detach(id) end)
    Jetstream.api!("$JS.API.CONSUMER.DELETE.P.once", %{})
    Jetstream.create_consumer!("P", "once")
    Process.sleep(200)
    %{pid: old} = Planes.connection(:jetstream)
    Process.exit(old, :kill)

    assert Jetstream.eventually(fn -> Pull.attached?(once) end, 100)
    assert_receive {:attach, :once_pull}, 1_000
    refute_receive {:attach, :once_pull}, 2_500
  end

  test "a missing consumer keeps attached? false" do
    missing = start_extra(:missing, "absent", fn message -> message end)
    refute Jetstream.eventually(fn -> Pull.attached?(missing) end, 15)
  end

  test "pause stops pulling and unsubscribes", %{pull: pull} do
    assert :ok = Pull.pause(pull)
    refute Pull.attached?(pull)
    Jetstream.publish!("p.after", "late")
    refute_receive {:handled, _task, "late"}, 1_200
  end

  test "pull re-attaches after the connection is replaced", %{pull: pull} do
    %{pid: old} = Planes.connection(:jetstream)
    Process.exit(old, :kill)
    assert Jetstream.eventually(fn -> not Pull.attached?(pull) end)
    assert Jetstream.eventually(fn -> Pull.attached?(pull) end, 100)
    Jetstream.publish!("p.again", "back")
    assert_receive {:handled, _task, "back"}, 3_000
  end

  defp start_extra(name, durable, handler, opts \\ []) do
    supervisor = :"#{name}_tasks"
    start_supervised!(Supervisor.child_spec({Task.Supervisor, name: supervisor}, id: supervisor))

    start_supervised!(
      {Pull,
       name: :"#{name}_pull",
       stream: Keyword.get(opts, :stream, "P"),
       durable: durable,
       handler: handler,
       task_supervisor: supervisor,
       expires_ns: Keyword.get(opts, :expires_ns, 10_000_000_000)}
    )
  end

  defp task_running?(supervisor), do: Supervisor.which_children(supervisor) != []
end
