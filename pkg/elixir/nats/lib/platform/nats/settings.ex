# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.Settings do
  @moduledoc "Shared connection settings with per-host TLS server identity."

  def ca_certs do
    case System.get_env("NATS_CA_PEM") do
      pem when is_binary(pem) and pem != "" ->
        pem |> :public_key.pem_decode() |> Enum.map(fn {_type, der, _info} -> der end)

      _ ->
        nil
    end
  end

  def credentials(jwt, seed, opts \\ [])

  def credentials(jwt, seed, _opts) when is_binary(jwt) and is_binary(seed),
    do: %{jwt: jwt, nkey_seed: seed}

  def credentials(nil, nil, _opts), do: %{}

  def credentials(_, _, opts) do
    if Keyword.get(opts, :strict, false) do
      raise "NATS_JWT and NATS_NKEY_SEED must both be set or both unset"
    end

    %{}
  end

  def tls(nil), do: nil

  def tls(cacerts) do
    {cacerts,
     client_files(
       System.get_env("NATS_CLIENT_CERT_FILE", ""),
       System.get_env("NATS_CLIENT_KEY_FILE", "")
     )}
  end

  def connection({host, port}, credentials, tls) do
    %{host: host, port: port, no_responders: true}
    |> Map.merge(credentials)
    |> Map.merge(tls_settings(host, tls))
  end

  defp client_files("", ""), do: []

  # Keep file paths so TLS can read renewed client certificates on reconnect.
  defp client_files(cert, key) when cert != "" and key != "", do: [certfile: cert, keyfile: key]

  defp client_files(_, _),
    do: raise("NATS_CLIENT_CERT_FILE and NATS_CLIENT_KEY_FILE must both be set or both empty")

  defp tls_settings(_host, nil), do: %{}

  defp tls_settings(host, {cacerts, files}) do
    ssl_opts = [
      verify: :verify_peer,
      cacerts: cacerts,
      server_name_indication: String.to_charlist(host),
      depth: 3
    ]

    %{tls: true, ssl_opts: ssl_opts ++ files}
  end
end
