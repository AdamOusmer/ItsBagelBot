defmodule Ingress.NatsFailbackTest do
  use ExUnit.Case, async: true

  alias Ingress.NatsFailback

  test "monitor health endpoint must return HTTP 200" do
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, reuseaddr: true])
    {:ok, port} = :inet.port(listener)

    server =
      Task.async(fn ->
        {:ok, socket} = :gen_tcp.accept(listener)
        {:ok, _request} = :gen_tcp.recv(socket, 0, 1_000)
        :ok = :gen_tcp.send(socket, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
        :gen_tcp.close(socket)
      end)

    assert NatsFailback.local_leaf_ready?("http://127.0.0.1:#{port}/healthz", 1_000)
    Task.await(server)
    :gen_tcp.close(listener)
  end

  test "unreachable monitor endpoint fails closed" do
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, reuseaddr: true])
    {:ok, port} = :inet.port(listener)
    :gen_tcp.close(listener)

    refute NatsFailback.local_leaf_ready?("http://127.0.0.1:#{port}/healthz", 100)
  end
end
