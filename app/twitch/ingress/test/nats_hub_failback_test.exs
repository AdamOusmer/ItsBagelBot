# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.NatsHubFailbackTest do
  use ExUnit.Case, async: false

  alias Ingress.NatsHubFailback

  describe "observe/3" do
    test "a local connection resets the streak and the backoff" do
      assert {false, %{consecutive: 0, failbacks: 0}} =
               NatsHubFailback.observe(%{consecutive: 2, failbacks: 3}, :local, 3)
    end

    test "an unknown locality resets the streak but keeps the backoff" do
      assert {false, %{consecutive: 0, failbacks: 2}} =
               NatsHubFailback.observe(%{consecutive: 2, failbacks: 2}, :unknown, 3)
    end

    test "a displaced connection recycles only after the required streak" do
      streak = NatsHubFailback.new_streak()
      assert {false, streak} = NatsHubFailback.observe(streak, :displaced, 2)

      assert {true, %{consecutive: 0, failbacks: 1}} =
               NatsHubFailback.observe(streak, :displaced, 2)
    end

    test "failbacks that land remote again double the streak, capped at 16x" do
      recycles =
        Enum.reduce(1..80, {NatsHubFailback.new_streak(), []}, fn tick, {streak, ticks} ->
          case NatsHubFailback.observe(streak, :displaced, 1) do
            {true, streak} -> {streak, [tick | ticks]}
            {false, streak} -> {streak, ticks}
          end
        end)
        |> elem(1)
        |> Enum.reverse()

      assert recycles == [1, 3, 7, 15, 31, 47, 63, 79]
    end
  end

  setup do
    remote = fake_hub("nats-0")
    local = fake_hub("nats-1")
    [remote: remote, local: local]
  end

  describe "failback loop" do
    test "a connection on a remote hub is recycled", %{remote: remote, local: local} do
      {conn, pid} = connect(remote)
      start_failback(conn, local)

      assert_receive {:DOWN, _, :process, ^pid, _}, 1_000
    end

    test "a connection already on the local hub stays put", %{local: local} do
      {conn, pid} = connect(local)
      start_failback(conn, local)

      refute_receive {:DOWN, _, :process, ^pid, _}, 400
    end

    test "an unreachable local hub keeps the connection", %{remote: remote} do
      {conn, pid} = connect(remote)
      {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false])
      {:ok, closed} = :inet.port(listener)
      :gen_tcp.close(listener)
      start_failback(conn, closed)

      refute_receive {:DOWN, _, :process, ^pid, _}, 400
    end
  end

  test "only one displaced connection moves per check", %{remote: remote, local: local} do
    {one, one_pid} = connect(remote)
    {two, two_pid} = connect(remote)
    test = self()

    start_supervised!(
      {NatsHubFailback,
       name: :hub_failback_one_at_a_time,
       enabled: true,
       connections: fn ->
         [{one, fn -> send(test, {:moved, one}) end}, {two, fn -> send(test, {:moved, two}) end}]
       end,
       probe_settings: %{host: "127.0.0.1", port: local, connection_timeout: 200},
       interval: 50,
       required: 2}
    )

    assert_receive {:moved, ^one}, 1_000
    refute_receive {:moved, ^two}, 30
    assert_receive {:moved, ^two}, 200
    assert Process.alive?(one_pid) and Process.alive?(two_pid)
  end

  describe "default connections" do
    setup do
      on_exit(fn -> :persistent_term.erase({Ingress.Nats.Publisher, :n}) end)
    end

    test "a single publisher shard stays in place" do
      :persistent_term.put({Ingress.Nats.Publisher, :n}, 1)
      assert [:gnat_bus] = Enum.map(NatsHubFailback.connections(), &elem(&1, 0))
    end

    test "every publisher shard is covered when the pool can spill" do
      :persistent_term.put({Ingress.Nats.Publisher, :n}, 3)

      assert [:gnat_bus, :gnat_bus_pub_0, :gnat_bus_pub_1, :gnat_bus_pub_2] =
               Enum.map(NatsHubFailback.connections(), &elem(&1, 0))
    end
  end

  test "it stays off without a node name or hub host" do
    assert :ignore = NatsHubFailback.init(enabled: false)
  end

  defp connect(port) do
    conn = :"hub_failback_test_#{System.unique_integer([:positive])}"
    {:ok, pid} = Gnat.start_link(%{host: "127.0.0.1", port: port}, name: conn)
    Process.unlink(pid)
    Process.monitor(pid)
    on_exit(fn -> if Process.alive?(pid), do: Gnat.stop(pid) end)
    {conn, pid}
  end

  defp start_failback(conn, local_port) do
    start_supervised!(
      {NatsHubFailback,
       name: :"#{conn}_failback",
       enabled: true,
       connections: fn -> [{conn, fn -> Gnat.stop(conn) end}] end,
       probe_settings: %{host: "127.0.0.1", port: local_port, connection_timeout: 200},
       interval: 30,
       required: 2}
    )
  end

  # Answers the NATS handshake under a fixed server name, like a hub behind its Service.
  defp fake_hub(server_name) do
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, reuseaddr: true])
    {:ok, port} = :inet.port(listener)

    info =
      ~s(INFO {"server_id":"#{server_name}","server_name":"#{server_name}","version":"2.11.0","proto":1,"headers":true,"max_payload":1048576}\r\n)

    acceptor = spawn(fn -> accept(listener, info) end)
    :ok = :gen_tcp.controlling_process(listener, acceptor)
    on_exit(fn -> Process.exit(acceptor, :kill) end)
    port
  end

  defp accept(listener, info) do
    case :gen_tcp.accept(listener) do
      {:ok, socket} ->
        handler = spawn(fn -> serve(socket) end)
        :ok = :gen_tcp.controlling_process(socket, handler)
        :ok = :gen_tcp.send(socket, info)
        send(handler, :go)
        accept(listener, info)

      {:error, _} ->
        :ok
    end
  end

  defp serve(socket) do
    receive do
      :go -> loop(socket)
    end
  end

  defp loop(socket) do
    case :gen_tcp.recv(socket, 0) do
      {:ok, data} ->
        if String.contains?(data, "PING"), do: :gen_tcp.send(socket, "PONG\r\n")
        loop(socket)

      {:error, _} ->
        :ok
    end
  end
end
