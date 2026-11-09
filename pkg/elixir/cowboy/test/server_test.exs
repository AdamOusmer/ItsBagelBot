defmodule Platform.Cowboy.ServerTest do
  use ExUnit.Case, async: false

  defmodule Handler do
    @behaviour Platform.Cowboy.Handler
    def init(state), do: {:ok, state}
    def handle_in({type, data}, state), do: {:push, {type, data}, state}
    def handle_info(_, state), do: {:ok, state}
    def terminate(_, _), do: :ok
  end

  defmodule Router do
    def init(opts), do: opts
    def call(conn, opts), do: Platform.Cowboy.Server.upgrade(conn, Handler, nil, opts)
  end

  defmodule Hold do
    def init(pid), do: pid

    def call(conn, pid) do
      send(pid, {:held, self()})

      receive do
        :release -> Plug.Conn.send_resp(conn, 200, "ok")
      after
        5_000 -> Plug.Conn.send_resp(conn, 500, "timeout")
      end
    end
  end

  test "pipelined data remains bounded while a request is held" do
    start_supervised!(
      {Platform.Cowboy.Server, plug: {Hold, self()}, port: 0, name: :test_pipeline}
    )

    port = Platform.Cowboy.Server.port(:test_pipeline)

    {:ok, socket} =
      :gen_tcp.connect(~c"127.0.0.1", port, [:binary, active: false, send_timeout: 500])

    :ok = :gen_tcp.send(socket, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
    assert_receive {:held, handler}, 2_000
    [connection] = :ranch.procs(:test_pipeline, :connections)
    _ = :gen_tcp.send(socket, :binary.copy("x", 4 * 1024 * 1024))
    Process.sleep(500)
    {:binary, binaries} = Process.info(connection, :binary)
    assert Enum.reduce(binaries, 0, fn {_, size, _}, total -> size + total end) < 1_048_576
    send(handler, :release)
    assert {:ok, response} = :gen_tcp.recv(socket, 0, 2_000)
    assert response =~ "200 OK"
    :gen_tcp.close(socket)
  end

  defmodule BadHandler do
    def init(result), do: result
    def handle_in(_, result), do: result
    def handle_info(_, result), do: result
    def terminate(_, _), do: :ok
  end

  test "outbound frames and callback results are bounded" do
    base = %{
      handler: BadHandler,
      state: nil,
      frame_limit: 8,
      rate: 50,
      mailbox: 10,
      second: nil,
      count: 0
    }

    assert {:ok, _, :hibernate} =
             Platform.Cowboy.Socket.websocket_init(%{base | state: {:ok, nil, :hibernate}})

    assert {:reply, {:close, 1009, _}, _} =
             Platform.Cowboy.Socket.websocket_init(%{
               base
               | state: {:push, {:binary, "123456789"}, nil}
             })

    assert {:reply, {:close, 1009, _}, _} =
             Platform.Cowboy.Socket.websocket_init(%{
               base
               | state: {:push, List.duplicate({:binary, "x"}, 17), nil}
             })

    assert {:reply, {:close, 1009, _}, _} =
             Platform.Cowboy.Socket.websocket_init(%{
               base
               | state: {:push, List.duplicate({:binary, "x"}, 100_000), nil}
             })

    assert {:reply, {:close, 1011, _}, _} =
             Platform.Cowboy.Socket.websocket_init(%{
               base
               | state: {:push, [{:binary, "x"} | :bad], nil}
             })

    assert {:reply, {:close, 1011, _}, _} =
             Platform.Cowboy.Socket.websocket_init(%{
               base
               | state: {:push, {:binary, ["x"]}, nil}
             })

    assert {:reply, {:close, 1011, _}, _} =
             Platform.Cowboy.Socket.websocket_init(%{base | state: {:stop, 1005, "invalid", nil}})

    assert {:reply, {:close, 1011, _}, _} =
             Platform.Cowboy.Socket.websocket_init(%{base | state: :garbage})
  end

  test "upgrade rejects unknown, duplicate, and excessive options" do
    conn = Plug.Test.conn(:get, "/")

    for opts <- [
          [compress: true],
          [idle_timeout: 1, idle_timeout: 2],
          [max_frame_bytes: 1_048_577]
        ] do
      assert_raise ArgumentError, fn ->
        Platform.Cowboy.Server.upgrade(conn, Handler, nil, opts)
      end
    end
  end

  test "HTTP/2 prior knowledge preface is rejected" do
    start_supervised!({Platform.Cowboy.Server, plug: {Router, []}, port: 0, name: :test_http2})

    {:ok, socket} =
      :gen_tcp.connect(~c"127.0.0.1", Platform.Cowboy.Server.port(:test_http2), [
        :binary,
        active: false
      ])

    :ok = :gen_tcp.send(socket, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
    result = :gen_tcp.recv(socket, 0, 2_000)
    assert result == {:error, :closed} or match?({:ok, "HTTP/1." <> _}, result)
    :gen_tcp.close(socket)
  end

  defmodule Health do
    def init(opts), do: opts
    def call(conn, _), do: Plug.Conn.send_resp(conn, 200, "ok\n")
  end

  test "health plug responds over HTTP and HTTPS" do
    start_supervised!(
      {Platform.Cowboy.Server,
       plug: Health, port: 0, name: :test_health_http, ip: {127, 0, 0, 1}, num_acceptors: 2}
    )

    {:ok, tcp} =
      :gen_tcp.connect(~c"127.0.0.1", Platform.Cowboy.Server.port(:test_health_http), [
        :binary,
        active: false
      ])

    :ok =
      :gen_tcp.send(tcp, "GET /healthz HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

    assert {:ok, response} = :gen_tcp.recv(tcp, 0, 2_000)
    assert response =~ "200 OK"
    assert response =~ "ok\n"
    :gen_tcp.close(tcp)

    dir = Path.join(System.tmp_dir!(), "cowboy-#{System.unique_integer([:positive])}")
    File.mkdir_p!(dir)
    on_exit(fn -> File.rm_rf!(dir) end)
    cert = Path.join(dir, "cert.pem")
    key = Path.join(dir, "key.pem")

    {_, 0} =
      System.cmd(
        "openssl",
        [
          "req",
          "-x509",
          "-newkey",
          "rsa:2048",
          "-nodes",
          "-keyout",
          key,
          "-out",
          cert,
          "-days",
          "1",
          "-subj",
          "/CN=localhost"
        ],
        stderr_to_stdout: true
      )

    start_supervised!(
      {Platform.Cowboy.Server,
       plug: Health,
       port: 0,
       name: :test_health_tls,
       scheme: :https,
       certfile: cert,
       keyfile: key,
       ip: {127, 0, 0, 1}}
    )

    {:ok, tls} =
      :ssl.connect(
        ~c"127.0.0.1",
        Platform.Cowboy.Server.port(:test_health_tls),
        [:binary, active: false, verify: :verify_none],
        2_000
      )

    :ok = :ssl.send(tls, "GET /healthz HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
    assert {:ok, response} = :ssl.recv(tls, 0, 2_000)
    assert response =~ "200 OK"
    assert response =~ "ok\n"
    :ssl.close(tls)
  end

  test "TLS needs a complete certificate and key pair" do
    for options <- [
          [scheme: :https, certfile: "cert"],
          [scheme: :https, keyfile: "key"],
          [scheme: :http, certfile: "cert", keyfile: "key"]
        ] do
      assert_raise ArgumentError, fn ->
        Platform.Cowboy.Server.child_spec([name: :invalid_tls, plug: Health] ++ options)
      end
    end
  end

  test "listener capacity is bounded and applied" do
    for value <- [0, -1, 1_000_001, :infinity] do
      assert_raise ArgumentError, fn ->
        Platform.Cowboy.Server.child_spec(
          name: :invalid_capacity,
          plug: Health,
          max_connections: value
        )
      end
    end

    start_supervised!(
      {Platform.Cowboy.Server, name: :capacity_test, port: 0, plug: Health, max_connections: 19}
    )

    assert :ranch.get_max_connections(:capacity_test) == 19
  end

  test "rejects unknown listener options" do
    assert_raise ArgumentError, fn ->
      Platform.Cowboy.Server.child_spec(name: :invalid, plug: Router, protocol_options: [])
    end
  end

  test "Cowboy upgrades a real connection and echoes a binary message" do
    _server =
      start_supervised!({Platform.Cowboy.Server, plug: {Router, []}, port: 0, name: :test_echo})

    port = Platform.Cowboy.Server.port(:test_echo)
    {:ok, client} = Platform.WebSocket.Client.connect("ws://127.0.0.1:#{port}/")
    {client, :upgraded} = await_event(client)
    {:ok, client} = Platform.WebSocket.Client.send_frame(client, {:binary, "hello"})
    {client, {:frame, {:binary, "hello"}}} = await_event(client)
    Platform.WebSocket.Client.close(client)
  end

  test "connection closes when the application message rate is exceeded" do
    _server =
      start_supervised!(
        {Platform.Cowboy.Server,
         plug: {Router, [max_messages_per_second: 1]}, port: 0, name: :test_rate}
      )

    port = Platform.Cowboy.Server.port(:test_rate)
    {:ok, client} = Platform.WebSocket.Client.connect("ws://127.0.0.1:#{port}/")
    {client, :upgraded} = await_event(client)
    {:ok, client} = Platform.WebSocket.Client.send_frame(client, {:binary, "one"})
    {client, {:frame, {:binary, "one"}}} = await_event(client)
    {:ok, client} = Platform.WebSocket.Client.send_frame(client, {:binary, "two"})
    {client, {:frame, {:close, 1008, _}}} = await_event(client)
    Platform.WebSocket.Client.close(client)
  end

  test "oversized frames are closed by Cowboy" do
    start_supervised!(
      {Platform.Cowboy.Server, plug: {Router, [max_frame_bytes: 16]}, port: 0, name: :test_size}
    )

    port = Platform.Cowboy.Server.port(:test_size)
    {:ok, client} = Platform.WebSocket.Client.connect("ws://127.0.0.1:#{port}/")
    {client, :upgraded} = await_event(client)

    {:ok, client} =
      Platform.WebSocket.Client.send_frame(client, {:binary, String.duplicate("x", 17)})

    {client, {:frame, {:close, 1009, _}}} = await_event(client)
    Platform.WebSocket.Client.close(client)
  end

  test "control frames preserve hibernation" do
    socket = %{state: :example}

    assert {:ok, ^socket, :hibernate} =
             Platform.Cowboy.Socket.websocket_handle({:ping, "x"}, socket)

    assert {:ok, ^socket, :hibernate} =
             Platform.Cowboy.Socket.websocket_handle({:pong, "x"}, socket)
  end

  test "fragmented message exceeding the aggregate limit closes 1009" do
    start_supervised!(
      {Platform.Cowboy.Server,
       plug: {Router, [max_frame_bytes: 16]}, port: 0, name: :test_fragments}
    )

    {:ok, socket} =
      :gen_tcp.connect(~c"127.0.0.1", Platform.Cowboy.Server.port(:test_fragments), [
        :binary,
        active: false
      ])

    :ok = :gen_tcp.send(socket, upgrade_request(""))
    assert {:ok, "HTTP/1.1 101" <> _} = :gen_tcp.recv(socket, 0, 2_000)
    # Client frames use a zero mask; each fragment is below the 16-byte limit.
    :ok =
      :gen_tcp.send(
        socket,
        <<2, 138, 0, 0, 0, 0, "1234567890", 128, 138, 0, 0, 0, 0, "1234567890">>
      )

    assert {:ok, <<136, _length, 1009::16, _::binary>>} = :gen_tcp.recv(socket, 0, 2_000)
    :gen_tcp.close(socket)
  end

  test "compression is not negotiated" do
    start_supervised!(
      {Platform.Cowboy.Server, plug: {Router, []}, port: 0, name: :test_compression}
    )

    port = Platform.Cowboy.Server.port(:test_compression)
    {:ok, socket} = :gen_tcp.connect(~c"127.0.0.1", port, [:binary, active: false])

    :ok =
      :gen_tcp.send(socket, upgrade_request("Sec-WebSocket-Extensions: permessage-deflate\r\n"))

    {:ok, response} = :gen_tcp.recv(socket, 0, 2_000)
    assert response =~ "101 Switching Protocols"
    refute String.downcase(response) =~ "sec-websocket-extensions"
    :gen_tcp.close(socket)
  end

  test "client rejects unknown and malformed schemes" do
    assert {:error, :invalid_url} = Platform.WebSocket.Client.connect("ftp://example.com")
    assert {:error, :invalid_url} = Platform.WebSocket.Client.connect("ws:///missing")
  end

  defp await_event(client) do
    receive do
      message ->
        case Platform.WebSocket.Client.stream(client, message) do
          {:ok, next, [event | _]} -> {next, event}
          {:ok, next, []} -> await_event(next)
          :unknown -> await_event(client)
          other -> flunk("socket failed: #{inspect(other)}")
        end
    after
      2_000 -> flunk("socket timed out")
    end
  end

  defp upgrade_request(extra_headers) do
    key = Base.encode64(:crypto.strong_rand_bytes(16))

    "GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" <>
      "Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: #{key}\r\n#{extra_headers}\r\n"
  end
end
