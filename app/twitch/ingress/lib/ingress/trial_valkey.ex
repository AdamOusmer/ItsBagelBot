# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialValkey do
  @moduledoc """
  Small RESP client for the trial control plane. Every call has its own bounded
  socket so a failed Valkey connection cannot hold the ingress hot path.
  """

  @timeout 2_000

  def command(parts) when is_list(parts) do
    with {:ok, host, port, transport, opts} <- endpoint(),
         {:ok, host, port} <- master_endpoint(host, port, transport, opts),
         {:ok, socket} <- transport.connect(host, port, opts, @timeout) do
      try do
        payload = ["*", Integer.to_string(length(parts)), "\r\n", Enum.map(parts, &bulk/1)]

        with :ok <- authenticate(transport, socket),
             :ok <- transport.send(socket, payload),
             {:ok, reply} <- read(transport, socket) do
          {:ok, reply}
        end
      after
        transport.close(socket)
      end
    end
  end

  defp master_endpoint(host, 26_380, transport, opts) do
    with {:ok, socket} <- transport.connect(host, 26_380, opts, @timeout) do
      try do
        with :ok <- authenticate(transport, socket),
             :ok <-
               transport.send(socket, [
                 "*3\r\n",
                 bulk("SENTINEL"),
                 bulk("get-master-addr-by-name"),
                 bulk("myprimary")
               ]),
             {:ok, [master, port]} <- read(transport, socket),
             {number, ""} <- Integer.parse(port) do
          {:ok, String.to_charlist(master), number}
        else
          _ -> {:error, :master_unavailable}
        end
      after
        transport.close(socket)
      end
    end
  end

  defp master_endpoint(host, port, _transport, _opts), do: {:ok, host, port}

  defp authenticate(transport, socket) do
    case System.get_env("VALKEY_PASSWORD") do
      nil ->
        :ok

      "" ->
        :ok

      password ->
        payload = ["*2\r\n", bulk("AUTH"), bulk(password)]

        with :ok <- transport.send(socket, payload),
             {:ok, "OK"} <- read(transport, socket),
             do: :ok
    end
  end

  defp endpoint do
    case System.get_env("VALKEY_ADDR") do
      nil ->
        {:error, :unconfigured}

      "" ->
        {:error, :unconfigured}

      "unix:" <> path ->
        {:ok, {:local, String.to_charlist(path)}, 0, :gen_tcp,
         [:binary, active: false, packet: :raw]}

      address ->
        case String.split(address, ":") do
          [host, port] ->
            case Integer.parse(port) do
              {number, ""} ->
                tls? =
                  System.get_env("VALKEY_TLS_CA_PEM") not in [nil, ""] or
                    System.get_env("VALKEY_TLS_CA_FILE") not in [nil, ""]

                if tls? do
                  ca_opts =
                    case System.get_env("VALKEY_TLS_CA_PEM") do
                      nil ->
                        [cacertfile: String.to_charlist(System.fetch_env!("VALKEY_TLS_CA_FILE"))]

                      pem ->
                        [
                          cacerts:
                            for({:Certificate, der, _} <- :public_key.pem_decode(pem), do: der)
                        ]
                    end

                  cert_opts =
                    case {System.get_env("VALKEY_TLS_CLIENT_CERT_FILE"),
                          System.get_env("VALKEY_TLS_CLIENT_KEY_FILE")} do
                      {nil, nil} ->
                        []

                      {cert, key} when is_binary(cert) and is_binary(key) ->
                        [certfile: String.to_charlist(cert), keyfile: String.to_charlist(key)]

                      _ ->
                        raise "VALKEY_TLS_CLIENT_CERT_FILE and VALKEY_TLS_CLIENT_KEY_FILE must be set together"
                    end

                  opts =
                    [
                      active: false,
                      mode: :binary,
                      verify: :verify_peer,
                      server_name_indication:
                        String.to_charlist(System.get_env("VALKEY_TLS_SERVER_NAME", host))
                    ] ++ ca_opts ++ cert_opts

                  {:ok, String.to_charlist(host), number, :ssl, opts}
                else
                  {:ok, String.to_charlist(host), number, :gen_tcp,
                   [:binary, active: false, packet: :raw]}
                end

              _ ->
                {:error, :invalid_address}
            end

          _ ->
            {:error, :invalid_address}
        end
    end
  end

  defp bulk(part) do
    data = to_string(part)
    ["$", Integer.to_string(byte_size(data)), "\r\n", data, "\r\n"]
  end

  defp read(transport, socket) do
    with {:ok, <<prefix>>} <- transport.recv(socket, 1, @timeout),
         {:ok, line} <- line(transport, socket, []) do
      case prefix do
        ?+ -> {:ok, line}
        ?- -> {:error, {:valkey, line}}
        ?: -> {:ok, String.to_integer(line)}
        ?$ -> read_bulk(transport, socket, String.to_integer(line))
        ?* -> read_array(transport, socket, String.to_integer(line), [])
        _ -> {:error, :invalid_reply}
      end
    end
  end

  defp read_bulk(_transport, _socket, -1), do: {:ok, nil}

  defp read_bulk(transport, socket, size) do
    with {:ok, value} <- transport.recv(socket, size + 2, @timeout) do
      {:ok, binary_part(value, 0, size)}
    end
  end

  defp read_array(_transport, _socket, 0, acc), do: {:ok, Enum.reverse(acc)}
  defp read_array(_transport, _socket, -1, _acc), do: {:ok, nil}

  defp read_array(transport, socket, n, acc) do
    with {:ok, value} <- read(transport, socket) do
      read_array(transport, socket, n - 1, [value | acc])
    end
  end

  defp line(transport, socket, acc) do
    case transport.recv(socket, 1, @timeout) do
      {:ok, "\r"} ->
        with {:ok, "\n"} <- transport.recv(socket, 1, @timeout) do
          {:ok, acc |> Enum.reverse() |> IO.iodata_to_binary()}
        end

      {:ok, byte} ->
        line(transport, socket, [byte | acc])

      error ->
        error
    end
  end
end
