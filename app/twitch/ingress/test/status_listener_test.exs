# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Ingress.StatusListenerTest do
  use ExUnit.Case, async: false

  setup do
    keys = ["STATUS_PORT", "TLS_CERT_FILE", "TLS_KEY_FILE"]
    previous = Map.new(keys, &{&1, System.get_env(&1)})
    System.put_env("STATUS_PORT", "0")
    System.delete_env("TLS_CERT_FILE")
    System.delete_env("TLS_KEY_FILE")

    on_exit(fn ->
      Enum.each(previous, fn {key, value} ->
        if value, do: System.put_env(key, value), else: System.delete_env(key)
      end)
    end)

    :ok
  end

  test "real HTTP serves liveness, readiness and status with two acceptors" do
    {Platform.Cowboy.Server, opts} = child = Ingress.Application.status_listener()
    assert opts[:num_acceptors] == 2
    assert opts[:scheme] == :http
    start_supervised!(child)
    port = Platform.Cowboy.Server.port(:ingress_status)
    assert request(:tcp, port, "/healthz") =~ "200 OK"
    assert request(:tcp, port, "/readyz") =~ "503 Service Unavailable"
    status = request(:tcp, port, "/status")
    assert status =~ "503 Service Unavailable"
    assert String.downcase(status) =~ "cache-control: no-store"
    assert status =~ ~s("service":"ingress")
    assert request(:tcp, port, "/unknown") =~ "404 Not Found"
  end

  test "real HTTPS status works and rejects partial TLS configuration" do
    dir = Path.join(System.tmp_dir!(), "ingress-status-#{System.unique_integer([:positive])}")
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

    System.put_env("TLS_CERT_FILE", cert)
    assert_raise RuntimeError, fn -> Ingress.Application.status_listener() end
    System.delete_env("TLS_CERT_FILE")
    System.put_env("TLS_KEY_FILE", key)
    assert_raise RuntimeError, fn -> Ingress.Application.status_listener() end
    System.put_env("TLS_CERT_FILE", cert)
    {Platform.Cowboy.Server, opts} = child = Ingress.Application.status_listener()
    assert opts[:scheme] == :https
    assert opts[:num_acceptors] == 2
    start_supervised!(child)
    port = Platform.Cowboy.Server.port(:ingress_status)

    for version <- [:"tlsv1.2", :"tlsv1.3"] do
      assert request({:tls, version}, port, "/healthz") =~ "200 OK"
      assert request({:tls, version}, port, "/status") =~ "503 Service Unavailable"
    end
  end

  defp request(transport, port, path) do
    {module, socket} =
      case transport do
        :tcp ->
          {:ok, sock} = :gen_tcp.connect(~c"127.0.0.1", port, [:binary, active: false], 2_000)
          {:gen_tcp, sock}

        {:tls, version} ->
          {:ok, sock} =
            :ssl.connect(
              ~c"127.0.0.1",
              port,
              [:binary, active: false, verify: :verify_none, versions: [version]],
              2_000
            )

          {:ssl, sock}
      end

    :ok =
      module.send(socket, "GET #{path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

    response = receive_all(module, socket, [])
    module.close(socket)
    response
  end

  defp receive_all(module, socket, acc) do
    case module.recv(socket, 0, 2_000) do
      {:ok, bytes} -> receive_all(module, socket, [bytes | acc])
      {:error, :closed} -> acc |> Enum.reverse() |> IO.iodata_to_binary()
      error -> flunk("HTTP read failed: #{inspect(error)}")
    end
  end
end
