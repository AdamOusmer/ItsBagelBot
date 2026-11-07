# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.RuntimeConfigTest do
  use ExUnit.Case, async: false

  @runtime "config/runtime.exs"
  @names ~w(NATS_JWT NATS_NKEY_SEED NATS_CA_PEM)

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

  test "production refuses to start without NATS auth and TLS" do
    assert_raise RuntimeError, ~r/production requires/, fn -> read(:prod) end
  end

  test "half-set NATS credentials are refused in every environment" do
    System.put_env("NATS_JWT", "jwt-without-seed")
    assert_raise RuntimeError, ~r/must both be set/, fn -> read(:test) end
  end

  test "development may still connect without credentials" do
    [settings] = read(:dev)[:controls_gateway][:nats]
    refute Map.has_key?(settings, :jwt)
    refute Map.has_key?(settings, :tls)
  end
end
