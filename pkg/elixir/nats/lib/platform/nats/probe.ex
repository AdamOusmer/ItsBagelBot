# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Nats.Probe do
  @moduledoc "Bounded health endpoint probe using caller-supplied TLS roots."
  def http_ready?(health_url, timeout, cacerts) do
    with %URI{scheme: scheme, host: host, port: port, path: path}
         when scheme in ["http", "https"] and is_binary(host) <- URI.parse(health_url),
         {:ok, transport, socket} <- connect(scheme, host, port, timeout, cacerts) do
      request = [
        "GET ",
        if(path in [nil, ""], do: "/healthz", else: path),
        " HTTP/1.1\r\nHost: ",
        host,
        "\r\nConnection: close\r\n\r\n"
      ]

      result =
        with :ok <- transport.send(socket, request),
             {:ok, response} <- transport.recv(socket, 0, timeout) do
          response
          |> :binary.split("\r\n", [:global])
          |> List.first()
          |> then(&(&1 in ["HTTP/1.1 200 OK", "HTTP/1.0 200 OK"]))
        else
          _ -> false
        end

      transport.close(socket)
      result
    else
      _ -> false
    end
  end

  defp connect("http", host, port, timeout, _cacerts) do
    with {:ok, socket} <-
           :gen_tcp.connect(String.to_charlist(host), port, [:binary, active: false], timeout) do
      {:ok, :gen_tcp, socket}
    end
  end

  defp connect("https", host, port, timeout, cacerts) do
    opts = [
      :binary,
      active: false,
      verify: :verify_peer,
      cacerts: cacerts,
      server_name_indication: String.to_charlist(host),
      depth: 3
    ]

    with {:ok, socket} <- :ssl.connect(String.to_charlist(host), port, opts, timeout) do
      {:ok, :ssl, socket}
    end
  end
end
