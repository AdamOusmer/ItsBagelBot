# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.NatsDualCredentialsTest do
  use ExUnit.Case, async: false

  @runtime "config/runtime.exs"

  setup do
    System.put_env("NATS_USER", "bus-user")
    System.put_env("NATS_PASSWORD", "bus-pass")

    keys = [
      "NATS_AUTH_MODE",
      "NATS_RPC_AUTH_MODE",
      "NATS_USER",
      "NATS_PASSWORD",
      "NATS_JWT",
      "NATS_NKEY_SEED",
      "NATS_RPC_USER",
      "NATS_RPC_PASSWORD",
      "NATS_RPC_JWT",
      "NATS_RPC_NKEY_SEED"
    ]

    on_exit(fn -> Enum.each(keys, &System.delete_env/1) end)
    :ok
  end

  describe "default password mode" do
    test "carries username/password on both planes, unchanged from today" do
      config = read_runtime()

      assert [%{username: "bus-user", password: "bus-pass"}] = config[:ingress][:nats_bus]
      assert [%{username: "bus-user", password: "bus-pass"}] = config[:ingress][:nats]
    end

    test "ignores JWT vars even when both are present" do
      System.put_env("NATS_JWT", "bus-jwt")
      System.put_env("NATS_NKEY_SEED", "bus-seed")

      config = read_runtime()

      refute Map.has_key?(List.first(config[:ingress][:nats_bus]), :jwt)
      assert [%{username: "bus-user", password: "bus-pass"}] = config[:ingress][:nats_bus]
    end
  end

  describe "NATS_AUTH_MODE=jwt" do
    setup do
      System.put_env("NATS_AUTH_MODE", "jwt")
      System.put_env("NATS_JWT", "bus-jwt")
      System.put_env("NATS_NKEY_SEED", "bus-seed")
      :ok
    end

    test "sends jwt and nkey_seed instead of username/password" do
      config = read_runtime()

      assert [%{jwt: "bus-jwt", nkey_seed: "bus-seed"}] = config[:ingress][:nats_bus]
      refute Map.has_key?(List.first(config[:ingress][:nats_bus]), :username)
    end

    test "the RPC plane falls back to the bus JWT vars exactly like the password logic" do
      config = read_runtime()

      assert [%{jwt: "bus-jwt", nkey_seed: "bus-seed"}] = config[:ingress][:nats]
    end

    test "an RPC-specific JWT pair wins over the bus fallback" do
      System.put_env("NATS_RPC_JWT", "rpc-jwt")
      System.put_env("NATS_RPC_NKEY_SEED", "rpc-seed")

      config = read_runtime()

      assert [%{jwt: "rpc-jwt", nkey_seed: "rpc-seed"}] = config[:ingress][:nats]
      assert [%{jwt: "bus-jwt", nkey_seed: "bus-seed"}] = config[:ingress][:nats_bus]
    end

    test "falls back to password when the JWT pair is incomplete" do
      System.delete_env("NATS_NKEY_SEED")

      config = read_runtime()

      assert [%{username: "bus-user", password: "bus-pass"}] = config[:ingress][:nats_bus]
    end
  end

  describe "per-plane NATS_RPC_AUTH_MODE" do
    test "defaults both planes to password when neither mode var is set" do
      config = read_runtime()

      assert [%{username: "bus-user", password: "bus-pass"}] = config[:ingress][:nats_bus]
      assert [%{username: "bus-user", password: "bus-pass"}] = config[:ingress][:nats]
    end

    test "switches the RPC plane to jwt while the bus plane stays on password" do
      System.put_env("NATS_RPC_AUTH_MODE", "jwt")
      System.put_env("NATS_RPC_JWT", "rpc-jwt")
      System.put_env("NATS_RPC_NKEY_SEED", "rpc-seed")

      config = read_runtime()

      assert [%{jwt: "rpc-jwt", nkey_seed: "rpc-seed"}] = config[:ingress][:nats]
      assert [%{username: "bus-user", password: "bus-pass"}] = config[:ingress][:nats_bus]
    end

    test "inherits the bus mode when NATS_RPC_AUTH_MODE is unset" do
      System.put_env("NATS_AUTH_MODE", "jwt")
      System.put_env("NATS_JWT", "bus-jwt")
      System.put_env("NATS_NKEY_SEED", "bus-seed")

      config = read_runtime()

      assert [%{jwt: "bus-jwt", nkey_seed: "bus-seed"}] = config[:ingress][:nats_bus]
      assert [%{jwt: "bus-jwt", nkey_seed: "bus-seed"}] = config[:ingress][:nats]
    end
  end

  defp read_runtime do
    {config, _warnings} =
      ExUnit.CaptureIO.with_io(:stderr, fn -> Config.Reader.read!(@runtime, env: :test) end)

    config
  end
end
