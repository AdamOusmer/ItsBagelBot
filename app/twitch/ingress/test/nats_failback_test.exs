# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

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

  describe "https monitor" do
    @describetag :tmp_dir

    setup %{tmp_dir: dir} do
      cert = Path.join(dir, "leaf.pem")
      key = Path.join(dir, "leaf.key")
      ca_cert = Path.join(dir, "ca.pem")
      ca_key = Path.join(dir, "ca.key")
      csr = Path.join(dir, "leaf.csr")
      ext = Path.join(dir, "leaf.ext")
      File.write!(ext, "subjectAltName=DNS:localhost")
      ec = "-newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes"

      script = """
      set -e
      openssl req -x509 #{ec} -days 1 -subj /CN=fleet-ca -keyout #{ca_key} -out #{ca_cert}
      openssl req -new #{ec} -subj /CN=leaf -keyout #{key} -out #{csr}
      openssl x509 -req -in #{csr} -CA #{ca_cert} -CAkey #{ca_key} -set_serial 1 -days 1 -extfile #{ext} -out #{cert}
      """

      {_, 0} = System.cmd("sh", ["-c", script], stderr_to_stdout: true)

      {:ok, listener} =
        :ssl.listen(0, [:binary, active: false, reuseaddr: true, certfile: cert, keyfile: key])

      {:ok, {_, port}} = :ssl.sockname(listener)

      server =
        Task.async(fn ->
          with {:ok, socket} <- :ssl.transport_accept(listener, 2_000),
               {:ok, socket} <- :ssl.handshake(socket, 2_000),
               {:ok, _request} <- :ssl.recv(socket, 0, 1_000) do
            :ssl.send(socket, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
            :ssl.close(socket)
          end
        end)

      on_exit(fn -> :ssl.close(listener) end)

      [
        port: port,
        ca: [ca_cert |> File.read!() |> :public_key.pem_decode() |> hd() |> elem(1)],
        server: server
      ]
    end

    test "a leaf signed by the fleet CA is ready", %{port: port, ca: ca, server: server} do
      assert NatsFailback.local_leaf_ready?("https://localhost:#{port}/healthz", 2_000, ca)
      Task.await(server)
    end

    test "a leaf whose name is not in the certificate is not ready", %{port: port, ca: ca} do
      refute NatsFailback.local_leaf_ready?("https://127.0.0.1:#{port}/healthz", 2_000, ca)
    end

    test "a leaf outside the fleet CA is not ready", %{port: port} do
      refute NatsFailback.local_leaf_ready?("https://localhost:#{port}/healthz", 2_000, [])
    end
  end
end
