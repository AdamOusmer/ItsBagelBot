defmodule Platform.WebSocket.ClientTest do
  use ExUnit.Case

  alias Platform.WebSocket.Client

  test "rejects unknown and malformed schemes" do
    assert {:error, :invalid_url} = Client.connect("ftp://example.com")
    assert {:error, :invalid_url} = Client.connect("ws:///missing")
  end

  test "sends request headers and keeps a frame coalesced with the upgrade" do
    {port, server} =
      serve(fn request, key ->
        assert request =~ ~r/\r\nx-test: value\r\n/i
        [switching(key), <<0x81, 5, "hello">>]
      end)

    {:ok, ws} = Client.connect("ws://127.0.0.1:#{port}/path?q=1", headers: [{"x-test", "value"}])
    assert {:ok, ws, [:upgraded, {:frame, {:text, "hello"}}]} = receive_stream(ws)
    assert {:ok, ws} = Client.send_frame(ws, {:text, "back"})
    assert :ok = Client.close(ws)
    assert Task.await(server) =~ "back"
  end

  test "exposes the connected peer and upgrade response headers" do
    {port, server} =
      serve(fn _request, key ->
        String.replace_suffix(switching(key), "\r\n", "x-amz-cf-pop: YUL62-P3\r\n\r\n")
      end)

    {:ok, ws} = Client.connect("ws://127.0.0.1:#{port}/")
    assert Client.peer(ws) == "127.0.0.1:#{port}"
    assert {:ok, ws, [:upgraded]} = receive_stream(ws)
    assert Client.response_header(ws, "x-amz-cf-pop") == "YUL62-P3"
    assert Client.response_header(ws, "via") == nil
    Client.close(ws)
    Task.await(server)
  end

  test "reports a refused upgrade by HTTP status" do
    {port, server} =
      serve(fn _request, _key ->
        "HTTP/1.1 401 Unauthorized\r\ncontent-length: 0\r\n\r\n"
      end)

    {:ok, ws} = Client.connect("ws://127.0.0.1:#{port}/")
    assert {:error, ws, {:http_status, 401}, []} = receive_stream(ws)
    assert {:error, :not_upgraded} = Client.send_frame(ws, {:text, "x"})
    Client.close(ws)
    Task.await(server)
  end

  test "connection failures carry the plain transport reason" do
    {:ok, listener} = :gen_tcp.listen(0, ip: {127, 0, 0, 1})
    {:ok, port} = :inet.port(listener)
    :gen_tcp.close(listener)

    assert {:error, :econnrefused} = Client.connect("ws://127.0.0.1:#{port}/")
  end

  test "reassembles fragments around control frames and frames split across reads" do
    long = :binary.copy("x", 300)
    message = <<0x82, 126, 300::16, long::binary>>

    {port, server} =
      serve(fn _request, key ->
        {:chunks,
         [
           switching(key),
           <<0x01, 3, "hel">>,
           <<0x89, 2, "hi">>,
           <<0x80, 2, "lo">>,
           binary_part(message, 0, 100),
           binary_part(message, 100, byte_size(message) - 100),
           <<0x81, 4, 0xC3, 0xA9, ?o, ?k>>,
           <<0x88, 2, 1001::16>>
         ]}
      end)

    {:ok, ws} = Client.connect("ws://127.0.0.1:#{port}/")

    assert {:ws, frames} = collect_frames(ws, 5, [])

    assert frames == [
             {:ping, "hi"},
             {:text, "hello"},
             {:binary, long},
             {:text, "éok"},
             {:close, 1001, ""}
           ]

    Task.await(server)
  end

  test "protocol violations fail the stream instead of producing frames" do
    for {bad, reason} <- [
          {<<0x81, 2, 0xFF, 0xFE>>, :invalid_utf8},
          {<<0x81, 0x81, 1, 2, 3, 4, ?a>>, :masked_server_frame},
          {<<0xC1, 1, ?a>>, :reserved_bits},
          {<<0x80, 1, ?a>>, :unexpected_continuation},
          {<<0x09, 0>>, :fragmented_control_frame},
          {<<0x88, 1, 3>>, :invalid_close_payload},
          {<<0x88, 2, 1005::16>>, :invalid_close_payload},
          {<<0x83, 0>>, {:unsupported_opcode, 3}}
        ] do
      {port, server} = serve(fn _request, key -> {:chunks, [switching(key), bad]} end)
      {:ok, ws} = Client.connect("ws://127.0.0.1:#{port}/")
      assert {:error, _ws, {:decode_failed, ^reason}, _events} = await_error(ws)
      Client.close(ws)
      Task.await(server)
    end
  end

  defp collect_frames(_ws, 0, acc), do: {:ws, Enum.reverse(acc)}

  defp collect_frames(ws, n, acc) do
    {:ok, ws, events} = receive_stream(ws)
    frames = for {:frame, frame} <- events, do: frame
    collect_frames(ws, n - length(frames), Enum.reverse(frames, acc))
  end

  defp await_error(ws) do
    case receive_stream(ws) do
      {:ok, ws, _events} -> await_error(ws)
      error -> error
    end
  end

  defp receive_stream(ws) do
    receive do
      message ->
        case Client.stream(ws, message) do
          :unknown -> receive_stream(ws)
          result -> result
        end
    after
      2_000 -> flunk("no websocket response")
    end
  end

  defp serve(respond) do
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, ip: {127, 0, 0, 1}])
    {:ok, port} = :inet.port(listener)

    server =
      Task.async(fn ->
        {:ok, socket} = :gen_tcp.accept(listener, 2_000)
        request = read_request(socket, "")
        [_, key] = Regex.run(~r/sec-websocket-key: ([^\r\n]+)/i, request)

        case respond.(request, key) do
          {:chunks, chunks} ->
            for chunk <- chunks do
              :ok = :gen_tcp.send(socket, chunk)
              Process.sleep(20)
            end

          response ->
            :ok = :gen_tcp.send(socket, response)
        end

        rest = drain(socket, "")
        :gen_tcp.close(socket)
        :gen_tcp.close(listener)
        unmask(rest)
      end)

    {port, server}
  end

  defp switching(key) do
    accept = :crypto.hash(:sha, key <> "258EAFA5-E914-47DA-95CA-C5AB0DC85B11") |> Base.encode64()

    "HTTP/1.1 101 Switching Protocols\r\nupgrade: websocket\r\nconnection: Upgrade\r\nsec-websocket-accept: #{accept}\r\n\r\n"
  end

  defp read_request(socket, buffer) do
    if String.contains?(buffer, "\r\n\r\n") do
      buffer
    else
      {:ok, data} = :gen_tcp.recv(socket, 0, 2_000)
      read_request(socket, buffer <> data)
    end
  end

  defp drain(socket, acc) do
    case :gen_tcp.recv(socket, 0, 500) do
      {:ok, data} -> drain(socket, acc <> data)
      {:error, _} -> acc
    end
  end

  defp unmask(
         <<_::1, _::3, _::4, 1::1, len::7, mask::binary-4, payload::binary-size(len),
           rest::binary>>
       )
       when len < 126 do
    data =
      for {byte, i} <- Enum.with_index(:binary.bin_to_list(payload)),
          into: <<>>,
          do: <<Bitwise.bxor(byte, :binary.at(mask, rem(i, 4)))>>

    data <> unmask(rest)
  end

  defp unmask(_), do: ""
end
