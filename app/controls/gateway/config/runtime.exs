# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

parse_keys = fn
  nil ->
    %{}

  "" ->
    %{}

  value ->
    value
    |> String.split(",")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(&(&1 == ""))
    |> Map.new(fn pair ->
      [kid, b64] = String.split(pair, ":", parts: 2)
      {kid, Base.decode64!(b64)}
    end)
end

config :controls_gateway,
  port: String.to_integer(System.get_env("CONTROLS_GATEWAY_PORT", "4000")),
  token_public_keys: parse_keys.(System.get_env("CONTROLS_TOKEN_PUBLIC_KEYS")),
  auth_token_url: System.get_env("DEVICE_AUTH_TOKEN_URL", ""),
  auth_revoke_url: System.get_env("DEVICE_AUTH_REVOKE_URL", ""),
  auth_prefix: System.get_env("NATS_OVERLAY_AUTH_SUBJECT_PREFIX", "bagel.rpc.overlay.device")

nats_host = System.get_env("NATS_HOST", "127.0.0.1")
nats_port = String.to_integer(System.get_env("NATS_PORT", "4222"))

nats_cacerts =
  case System.get_env("NATS_CA_PEM") do
    pem when is_binary(pem) and pem != "" ->
      pem |> :public_key.pem_decode() |> Enum.map(fn {_type, der, _info} -> der end)

    _ ->
      nil
  end

nats_auth =
  case {System.get_env("NATS_JWT"), System.get_env("NATS_NKEY_SEED")} do
    {jwt, seed} when is_binary(jwt) and is_binary(seed) -> %{nkey_seed: seed, jwt: jwt}
    {nil, nil} -> %{}
    _ -> raise "NATS_JWT and NATS_NKEY_SEED must both be set or both unset"
  end

if config_env() == :prod and (nats_auth == %{} or is_nil(nats_cacerts)) do
  raise "production requires NATS_JWT, NATS_NKEY_SEED and NATS_CA_PEM"
end

nats_tls =
  if nats_cacerts do
    ssl_opts = [
      verify: :verify_peer,
      cacerts: nats_cacerts,
      server_name_indication: String.to_charlist(nats_host),
      depth: 3
    ]

    # File paths, not contents: ssl re-reads cert-manager renewals.
    ssl_opts =
      case {System.get_env("NATS_CLIENT_CERT_FILE", ""),
            System.get_env("NATS_CLIENT_KEY_FILE", "")} do
        {"", ""} -> ssl_opts
        {cert, key} when cert != "" and key != "" -> ssl_opts ++ [certfile: cert, keyfile: key]
        _ -> raise "NATS_CLIENT_CERT_FILE and NATS_CLIENT_KEY_FILE must both be set or both empty"
      end

    %{tls: true, ssl_opts: ssl_opts}
  else
    %{}
  end

config :controls_gateway,
  nats: [
    Map.merge(
      %{host: nats_host, port: nats_port, no_responders: true},
      Map.merge(nats_auth, nats_tls)
    )
  ]

if level = System.get_env("LOG_LEVEL") do
  config :logger, level: String.to_existing_atom(level)
end
