# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

alias Platform.Nats.{Planes, Settings}

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

listener_tls =
  case {System.get_env("TLS_CERT_FILE", ""), System.get_env("TLS_KEY_FILE", "")} do
    {"", ""} -> []
    {cert, key} when cert != "" and key != "" -> [certfile: cert, keyfile: key]
    _ -> raise "TLS_CERT_FILE and TLS_KEY_FILE must both be set or both unset"
  end

cookie_key = System.get_env("OVERLAY_COOKIE_KEY")

if config_env() == :prod and (is_nil(cookie_key) or byte_size(cookie_key) < 32) do
  raise "production requires OVERLAY_COOKIE_KEY with at least 32 bytes"
end

config :controls_gateway,
  listener_tls: listener_tls,
  overlay_cookie_key: cookie_key,
  overlay_trust_cloudflare_ip: System.get_env("OVERLAY_TRUST_CLOUDFLARE_IP") == "true",
  powered_dev_frame_ancestors: System.get_env("OVERLAY_POWERED_DEV_FRAME_ANCESTORS") == "true",
  port: String.to_integer(System.get_env("CONTROLS_GATEWAY_PORT", "4000")),
  token_public_keys: parse_keys.(System.get_env("CONTROLS_TOKEN_PUBLIC_KEYS"))

nats_host = System.get_env("NATS_HOST", "127.0.0.1")
nats_port = String.to_integer(System.get_env("NATS_PORT", "4222"))

nats_cacerts = Settings.ca_certs()

nats_auth =
  Settings.credentials(System.get_env("NATS_JWT"), System.get_env("NATS_NKEY_SEED"), strict: true)

if config_env() == :prod and (nats_auth == %{} or is_nil(nats_cacerts)) do
  raise "production requires NATS_JWT, NATS_NKEY_SEED and NATS_CA_PEM"
end

nats_rpc_host = System.get_env("NATS_RPC_HOST", nats_host)
nats_rpc_port = String.to_integer(System.get_env("NATS_RPC_PORT", Integer.to_string(nats_port)))

endpoints = %{rpc: {nats_rpc_host, nats_rpc_port}, jetstream: {nats_host, nats_port}}
config :controls_gateway, nats: Planes.settings(endpoints, nats_auth, Settings.tls(nats_cacerts))

config :new_relic_agent, harvest_enabled: false, license_key: nil

if config_env() == :prod do
  config :logger, :default_formatter, format: {ControlsGateway.LogFormatter, :format}
end

if level = System.get_env("LOG_LEVEL") do
  config :logger, level: String.to_existing_atom(level)
end
