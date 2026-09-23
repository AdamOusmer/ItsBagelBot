# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialValkey do
  @moduledoc """
  Shared Redix connection for trial admission and owner fencing. Redix handles
  Sentinel discovery, reconnects, TLS, authentication, and RESP encoding.
  """

  @timeout 2_000
  @sentinel_port 26_380

  def child_spec(opts) do
    %{id: __MODULE__, start: {__MODULE__, :start_link, [opts]}}
  end

  def start_link(opts \\ []) do
    case connection_options() do
      {:ok, config} -> Redix.start_link(config ++ [name: __MODULE__] ++ opts)
      {:error, :unconfigured} -> :ignore
      error -> error
    end
  end

  def command(parts) when is_list(parts) do
    case Process.whereis(__MODULE__) do
      nil -> {:error, :unavailable}
      conn -> Redix.command(conn, parts, timeout: @timeout)
    end
  end

  defp connection_options do
    case System.get_env("VALKEY_ADDR") do
      nil -> {:error, :unconfigured}
      "" -> {:error, :unconfigured}
      address -> parse_address(address)
    end
  end

  defp parse_address(address) do
    with [host, port] <- String.split(address, ":"),
         {number, ""} <- Integer.parse(port) do
      {:ok, endpoint_options(host, number) ++ security_options()}
    else
      _ -> {:error, :invalid_address}
    end
  end

  defp endpoint_options(host, @sentinel_port) do
    sentinel =
      [
        sentinels: [[host: host, port: @sentinel_port]],
        group: "myprimary",
        role: :primary,
        timeout: @timeout
      ] ++ security_options()

    [sentinel: sentinel]
  end

  defp endpoint_options(host, port), do: [host: host, port: port]

  defp security_options do
    tls_options() ++ password_option()
  end

  defp password_option do
    case System.get_env("VALKEY_PASSWORD") do
      nil -> []
      "" -> []
      _ -> [password: {System, :fetch_env!, ["VALKEY_PASSWORD"]}]
    end
  end

  defp tls_options do
    if tls_enabled?(),
      do: [ssl: true, socket_opts: ca_options() ++ client_cert_options() ++ sni_options()],
      else: []
  end

  defp tls_enabled? do
    case System.get_env("VALKEY_TLS_CA_PEM") do
      nil -> ca_file?()
      "" -> ca_file?()
      _ -> true
    end
  end

  defp ca_file?, do: System.get_env("VALKEY_TLS_CA_FILE") not in [nil, ""]

  defp ca_options do
    case System.get_env("VALKEY_TLS_CA_PEM") do
      nil -> [cacertfile: String.to_charlist(System.fetch_env!("VALKEY_TLS_CA_FILE"))]
      "" -> [cacertfile: String.to_charlist(System.fetch_env!("VALKEY_TLS_CA_FILE"))]
      pem -> [cacerts: for({:Certificate, der, _} <- :public_key.pem_decode(pem), do: der)]
    end
  end

  defp client_cert_options do
    case {System.get_env("VALKEY_TLS_CLIENT_CERT_FILE"),
          System.get_env("VALKEY_TLS_CLIENT_KEY_FILE")} do
      {nil, nil} ->
        []

      {cert, key} when is_binary(cert) and is_binary(key) ->
        [certfile: String.to_charlist(cert), keyfile: String.to_charlist(key)]

      _ ->
        raise "VALKEY_TLS_CLIENT_CERT_FILE and VALKEY_TLS_CLIENT_KEY_FILE must be set together"
    end
  end

  defp sni_options do
    case System.get_env("VALKEY_TLS_SERVER_NAME") do
      nil -> []
      "" -> []
      name -> [server_name_indication: String.to_charlist(name)]
    end
  end
end
