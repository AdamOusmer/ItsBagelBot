# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.NatsCredentialsTest do
  use ExUnit.Case, async: false

  @runtime "config/runtime.exs"
  @keys [
    "NATS_USER",
    "NATS_PASSWORD",
    "NATS_JWT",
    "NATS_NKEY_SEED",
    "NATS_RPC_JWT",
    "NATS_RPC_NKEY_SEED"
  ]

  setup do
    System.put_env("NATS_JWT", "bus-jwt")
    System.put_env("NATS_NKEY_SEED", "bus-seed")
    on_exit(fn -> Enum.each(@keys, &System.delete_env/1) end)
    :ok
  end

  test "both planes send the JWT and seed, never a leftover password" do
    System.put_env("NATS_USER", "bus-user")
    System.put_env("NATS_PASSWORD", "bus-pass")

    config = read_runtime()

    for plane <- [:nats, :nats_bus] do
      assert [%{jwt: "bus-jwt", nkey_seed: "bus-seed"} = server] = config[:ingress][plane]
      refute Map.has_key?(server, :username)
      refute Map.has_key?(server, :password)
    end
  end

  test "an RPC-specific JWT pair wins over the bus pair on the RPC plane only" do
    System.put_env("NATS_RPC_JWT", "rpc-jwt")
    System.put_env("NATS_RPC_NKEY_SEED", "rpc-seed")

    config = read_runtime()

    assert [%{jwt: "rpc-jwt", nkey_seed: "rpc-seed"}] = config[:ingress][:nats]
    assert [%{jwt: "bus-jwt", nkey_seed: "bus-seed"}] = config[:ingress][:nats_bus]
  end

  test "an incomplete pair sends no credentials" do
    System.delete_env("NATS_NKEY_SEED")

    config = read_runtime()

    refute Map.has_key?(List.first(config[:ingress][:nats_bus]), :jwt)
  end

  defp read_runtime do
    {config, _warnings} =
      ExUnit.CaptureIO.with_io(:stderr, fn -> Config.Reader.read!(@runtime, env: :test) end)

    config
  end
end
