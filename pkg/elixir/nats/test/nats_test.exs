defmodule Platform.NatsTest do
  use ExUnit.Case, async: false

  setup do
    exe = System.find_executable("nats-server") || raise "nats-server required"
    {:ok, socket} = :gen_tcp.listen(0, [])
    {:ok, port} = :inet.port(socket)
    :gen_tcp.close(socket)

    server =
      Port.open({:spawn_executable, exe}, [
        :binary,
        :exit_status,
        args: ["-p", Integer.to_string(port), "-a", "127.0.0.1"]
      ])

    {:os_pid, os_pid} = :erlang.port_info(server, :os_pid)
    on_exit(fn -> System.cmd("kill", [Integer.to_string(os_pid)]) end)

    await(fn ->
      case :gen_tcp.connect({127, 0, 0, 1}, port, [], 100) do
        {:ok, conn} ->
          :gen_tcp.close(conn)
          true

        _ ->
          false
      end
    end)

    start_supervised!(
      Platform.Nats.connection_child(:test_nats, :test_nats, [%{host: "127.0.0.1", port: port}])
    )

    await(fn -> Platform.Nats.connected?(:test_nats) end)
    :ok
  end

  defmodule Echo do
    use Platform.Nats.Server
    @impl true
    def request(%{body: body}), do: {:reply, body}
  end

  test "builder bounds configuration and starts a generic consumer" do
    assert_raise ArgumentError, fn ->
      apply(Platform.Nats, :connection_child, [:bad, :test, [], []])
    end

    assert_raise ArgumentError, fn ->
      Platform.Nats.connection_child(:bad, :test, [%{}], backoff_period: 0)
    end

    start_supervised!(
      Platform.Nats.consumer_child(:echo, :test_nats, Echo, [%{topic: "test.echo"}])
    )

    assert {:ok, %{body: "echo"}} =
             Platform.Nats.request(:test_nats, "test.echo", "echo", receive_timeout: 1_000)
  end

  test "subscription, bounded publish, headers, and request reply" do
    pid = Platform.Nats.connection_pid(:test_nats)
    assert is_pid(pid)
    assert {:ok, sid} = Platform.Nats.subscribe(pid, self(), "test.subject")

    assert :ok =
             Platform.Nats.Wire.publish(
               pid,
               {"test.subject", ["hello", " world"], [headers: [{"X-Test", "value"}]]},
               500
             )

    assert_receive {:msg, %{body: "hello world", headers: headers}}, 2_000
    assert headers != nil
    assert :ok = Platform.Nats.unsubscribe(pid, sid)

    assert {:ok, sid} = Platform.Nats.subscribe(pid, self(), "test.rpc")

    task =
      Task.async(fn ->
        Platform.Nats.request(pid, "test.rpc", "request", receive_timeout: 1_000)
      end)

    assert_receive {:msg, %{body: "request", reply_to: reply}}, 1_000
    assert :ok = Platform.Nats.publish(pid, reply, "response")
    assert {:ok, %{body: "response"}} = Task.await(task)
    assert :ok = Platform.Nats.unsubscribe(pid, sid)
  end

  test "request and wire publish enforce finite timeouts" do
    pid = Platform.Nats.connection_pid(:test_nats)

    for timeout <- [:infinity, 0, -1, 60_001, nil] do
      assert {:error, :invalid_timeout} =
               Platform.Nats.request(pid, "test.invalid", "x", receive_timeout: timeout)

      assert {:error, :invalid_timeout} =
               Platform.Nats.Wire.publish(pid, {"test.invalid", "x", []}, timeout)
    end

    assert {:ok, sid} = Platform.Nats.subscribe(pid, self(), "test.max")
    assert :ok = Platform.Nats.Wire.publish(pid, {"test.max", "x", []}, 60_000)
    assert_receive {:msg, %{body: "x"}}, 1_000
    assert :ok = Platform.Nats.unsubscribe(pid, sid)

    start_supervised!(
      Platform.Nats.consumer_child(:max_echo, :test_nats, Echo, [%{topic: "test.max.echo"}])
    )

    assert {:ok, %{body: "x"}} =
             Platform.Nats.request(pid, "test.max.echo", "x", receive_timeout: 60_000)
  end

  test "timed publish fails promptly for a stalled connection" do
    pid =
      spawn(fn ->
        receive do
          _ -> Process.sleep(1_000)
        end
      end)

    assert {:error, :not_connected} =
             Platform.Nats.Wire.publish(pid, {"test.subject", "x", []}, 20)
  end

  test "wire publish carries reply and headers and rejects malformed commands" do
    pid = Platform.Nats.connection_pid(:test_nats)
    assert {:ok, sid} = Platform.Nats.subscribe(pid, self(), "test.shape")

    assert :ok =
             Platform.Nats.Wire.publish(
               pid,
               {"test.shape", "x", [reply_to: "test.reply", headers: [{"a", "1"}]]},
               500
             )

    assert_receive {:msg, %{body: "x", reply_to: "test.reply", headers: [{"a", "1"}]}}, 1_000

    assert :ok =
             Platform.Nats.Wire.publish(pid, {"test.shape", "y", [reply_to: "test.reply"]}, 500)

    assert_receive {:msg, %{body: "y", reply_to: "test.reply"}}, 1_000

    for {subject, opts} <- [
          {"bad subject", []},
          {"", []},
          {nil, []},
          {"test.shape", [reply_to: "bad reply"]},
          {"test.shape", [queue_group: "x"]},
          {"test.shape", [headers: [{"a", "1\r\nPUB test.shape 1"}]]},
          {"test.shape", [headers: [{"a b", "1"}]]},
          {"test.shape", [headers: [{"a", "1"}], headers: [{"b", "2"}]]}
        ] do
      assert {:error, :invalid_publish} =
               Platform.Nats.Wire.publish(pid, {subject, "z", opts}, 500)
    end

    refute_receive {:msg, _}, 100
    assert Process.alive?(pid)
    assert :ok = Platform.Nats.unsubscribe(pid, sid)
  end

  test "server name and stop tolerate a missing connection" do
    assert is_binary(Platform.Nats.server_name(:test_nats))
    assert Platform.Nats.server_name(:missing_nats) == nil
    assert Platform.Nats.stop(:missing_nats) == :ok
  end

  defp await(fun, attempts \\ 100)
  defp await(_, 0), do: flunk("timed out")

  defp await(fun, n),
    do:
      if(fun.(),
        do: :ok,
        else:
          (
            Process.sleep(20)
            await(fun, n - 1)
          )
      )
end
