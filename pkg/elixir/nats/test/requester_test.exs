# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.RequesterTest do
  use ExUnit.Case, async: false

  alias Platform.Nats
  alias Platform.Nats.{Planes, Requester, Settings}
  alias Platform.Nats.TestSupport.{Jetstream, NatsServer}

  @moduletag :tmp_dir
  @prefix "test.reply"

  setup %{tmp_dir: dir} do
    %{jetstream: endpoint} = NatsServer.start_planes(dir)
    settings = Settings.connection(endpoint, %{}, nil)
    start_supervised!(Nats.connection_child(:responder, :responder, [settings]))
    NatsServer.await_connection(:responder)
    {:ok, _sid} = Nats.subscribe(:responder, self(), "svc.echo")
    {:ok, _sid} = Nats.subscribe(:responder, self(), "svc.silent")
    start_supervised!({Requester, name: :requester, prefix: @prefix})
    assert Jetstream.eventually(&attached?/0)
    :ok
  end

  defp attached?,
    do: Requester.request(:requester, "svc.nobody", "", 500) == {:error, :no_responders}

  defp ask(subject, timeout_ms, mode) do
    task = Task.async(fn -> Requester.request(:requester, subject, "ping", timeout_ms) end)

    if mode == :answer do
      assert_receive {:msg, %{reply_to: reply}}, 1_000
      :ok = Nats.publish(:responder, reply, "pong")
    end

    Task.await(task)
  end

  test "replies arrive on a subject under the prefix" do
    task = Task.async(fn -> Requester.request(:requester, "svc.echo", "ping", 1_000) end)
    assert_receive {:msg, %{reply_to: reply, body: "ping"}}, 1_000
    assert String.starts_with?(reply, @prefix <> ".")
    :ok = Nats.publish(:responder, reply, "pong")
    assert {:ok, "pong"} = Task.await(task)
  end

  test "failed requests map to timeout and no_responders" do
    assert {:error, :timeout} = ask("svc.silent", 150, :ignore)
    assert {:error, :no_responders} = ask("svc.nobody", 1_000, :ignore)
  end

  test "concurrent requests each get their own reply" do
    tasks =
      for body <- ["a", "b"],
          do: Task.async(fn -> Requester.request(:requester, "svc.echo", body, 1_000) end)

    for _ <- 1..2 do
      assert_receive {:msg, %{reply_to: reply, body: body}}, 1_000
      :ok = Nats.publish(:responder, reply, "re:" <> body)
    end

    assert Enum.map(tasks, &Task.await/1) == [{:ok, "re:a"}, {:ok, "re:b"}]
  end

  test "waiters are released with not_connected when the connection goes down" do
    task = Task.async(fn -> Requester.request(:requester, "svc.silent", "ping", 5_000) end)
    assert_receive {:msg, %{body: "ping"}}, 1_000
    %{pid: pid} = Planes.connection(:jetstream)
    Process.exit(pid, :kill)
    assert {:error, :not_connected} = Task.await(task)
  end

  test "a late reply after timeout never reaches a later caller" do
    first = Task.async(fn -> Requester.request(:requester, "svc.echo", "first", 150) end)
    assert_receive {:msg, %{reply_to: stale, body: "first"}}, 1_000
    assert {:error, :timeout} = Task.await(first)

    second = Task.async(fn -> Requester.request(:requester, "svc.echo", "second", 1_000) end)
    assert_receive {:msg, %{reply_to: fresh, body: "second"}}, 1_000
    :ok = Nats.publish(:responder, stale, "late")
    :ok = Nats.publish(:responder, fresh, "real")
    assert {:ok, "real"} = Task.await(second)
  end

  test "requests work again after the connection is replaced" do
    %{pid: old} = Planes.connection(:jetstream)
    Process.exit(old, :kill)
    assert Jetstream.eventually(fn -> not attached?() end)
    assert Jetstream.eventually(&attached?/0, 100)
    assert {:ok, "pong"} = ask("svc.echo", 1_000, :answer)
  end
end
