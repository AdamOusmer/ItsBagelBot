defmodule Platform.NatsTest do
  use ExUnit.Case, async: false

  alias Platform.Nats.TestSupport.NatsServer

  setup do
    port = NatsServer.start()

    start_supervised!(
      Platform.Nats.connection_child(:test_nats, :test_nats, [%{host: "127.0.0.1", port: port}])
    )

    NatsServer.await_connection(:test_nats)
    :ok
  end

  defmodule Echo do
    use Platform.Nats.Server
    @impl true
    def request(%{body: body}), do: {:reply, body}
  end

  defmodule TraceEcho do
    use Platform.Nats.Server
    @impl true
    def request(message), do: {:reply, inspect(Map.get(message, :headers))}
  end

  test "servers continue caller traces and requests keep caller headers" do
    start_supervised!(
      Platform.Nats.consumer_child(:trace_echo, :test_nats, TraceEcho, [%{topic: "test.trace"}])
    )

    traceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

    assert {:ok, %{body: traced}} =
             Platform.Nats.request(:test_nats, "test.trace", "x",
               receive_timeout: 1_000,
               headers: [{"traceparent", traceparent}]
             )

    assert traced =~ traceparent
    assert {:ok, %{body: "nil"}} = Platform.Nats.request(:test_nats, "test.trace", "x")
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

  test "pipelined publish keeps wire order and answers every message" do
    pid = Platform.Nats.connection_pid(:test_nats)
    assert {:ok, sid} = Platform.Nats.subscribe(pid, self(), "test.many")
    messages = for n <- 1..40, do: {"test.many", Integer.to_string(n), [reply_to: "test.r"]}

    assert Platform.Nats.Wire.publish_many(pid, messages ++ [{"bad subject", "x", []}], 500) ==
             List.duplicate(:ok, 40) ++ [{:error, :invalid_publish}]

    bodies = for _ <- 1..40, do: receive_body()
    assert bodies == Enum.map(1..40, &Integer.to_string/1)
    assert :ok = Platform.Nats.unsubscribe(pid, sid)
  end

  test "pipelined publish shares one deadline across a stalled connection" do
    pid = spawn(fn -> Process.sleep(1_000) end)
    started = System.monotonic_time(:millisecond)

    assert Platform.Nats.Wire.publish_many(pid, List.duplicate({"test.s", "x", []}, 5), 50) ==
             List.duplicate({:error, :not_connected}, 5)

    assert System.monotonic_time(:millisecond) - started < 200
  end

  defp receive_body do
    assert_receive {:msg, %{body: body}}, 1_000
    body
  end

  test "server name and stop tolerate a missing connection" do
    assert is_binary(Platform.Nats.server_name(:test_nats))
    assert Platform.Nats.server_name(:missing_nats) == nil
    assert Platform.Nats.stop(:missing_nats) == :ok
  end
end
