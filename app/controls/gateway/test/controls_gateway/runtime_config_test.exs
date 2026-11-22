# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.RuntimeConfigTest do
  use ExUnit.Case, async: false

  @runtime "config/runtime.exs"
  @names ~w(
    NATS_JWT NATS_NKEY_SEED NATS_CA_PEM NATS_CLIENT_CERT_FILE NATS_CLIENT_KEY_FILE
    NATS_HOST NATS_PORT NATS_RPC_HOST NATS_RPC_PORT OVERLAY_COOKIE_KEY
  )

  setup do
    saved = Map.new(@names, &{&1, System.get_env(&1)})
    Enum.each(@names, &System.delete_env/1)

    on_exit(fn ->
      Enum.each(saved, fn
        {name, nil} -> System.delete_env(name)
        {name, value} -> System.put_env(name, value)
      end)
    end)
  end

  defp read(env), do: Config.Reader.read!(@runtime, env: env)

  test "production requires a cookie key of at least 32 bytes" do
    assert_raise RuntimeError, ~r/production requires OVERLAY_COOKIE_KEY/, fn -> read(:prod) end
  end

  test "production refuses to start without NATS auth and TLS" do
    System.put_env("OVERLAY_COOKIE_KEY", String.duplicate("a", 32))
    assert_raise RuntimeError, ~r/production requires NATS_JWT/, fn -> read(:prod) end
  end

  test "half-set NATS credentials are refused in every environment" do
    System.put_env("NATS_JWT", "jwt-without-seed")
    assert_raise RuntimeError, ~r/must both be set/, fn -> read(:test) end
  end

  test "development may still connect without credentials" do
    %{rpc: [rpc], jetstream: [jetstream]} = read(:dev)[:controls_gateway][:nats]
    assert rpc == jetstream
    refute Map.has_key?(jetstream, :jwt)
    refute Map.has_key?(jetstream, :tls)
  end

  test "each plane uses its configured endpoint as the TLS server name" do
    der = <<1, 2, 3>>

    System.put_env(%{
      "NATS_HOST" => "hub.example.test",
      "NATS_PORT" => "4223",
      "NATS_RPC_HOST" => "leaf.example.test",
      "NATS_RPC_PORT" => "4224",
      "NATS_JWT" => "user-jwt",
      "NATS_NKEY_SEED" => "user-seed",
      "NATS_CA_PEM" => :public_key.pem_encode([{:Certificate, der, :not_encrypted}]),
      "NATS_CLIENT_CERT_FILE" => "client.crt",
      "NATS_CLIENT_KEY_FILE" => "client.key"
    })

    settings = read(:dev)[:controls_gateway][:nats]

    endpoints = %{rpc: {"leaf.example.test", 4224}, jetstream: {"hub.example.test", 4223}}

    for {plane, {host, port}} <- endpoints do
      [conn] = Map.fetch!(settings, plane)

      assert %{host: ^host, port: ^port, jwt: "user-jwt", nkey_seed: "user-seed", tls: true} =
               conn

      assert conn.ssl_opts == [
               verify: :verify_peer,
               cacerts: [der],
               server_name_indication: String.to_charlist(host),
               depth: 3,
               certfile: "client.crt",
               keyfile: "client.key"
             ]
    end
  end
end
