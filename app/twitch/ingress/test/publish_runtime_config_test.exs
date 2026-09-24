# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.PublishRuntimeConfigTest do
  use ExUnit.Case, async: false

  import ExUnit.CaptureIO

  @runtime "config/runtime.exs"

  describe "INGRESS_PUBLISH_WIRE" do
    test "the shipped default is the atomic wire" do
      assert {config, ""} = read_runtime()
      assert config[:ingress][:publish_wire] == :atomic
      assert config[:ingress][:publish_wire] == Ingress.Config.Publish.wire()
    end

    test "the compatibility fallback is selectable without a warning" do
      System.put_env("INGRESS_PUBLISH_WIRE", "single")

      assert {config, ""} = read_runtime()
      assert config[:ingress][:publish_wire] == :single
    end

    test "an unrecognized value warns instead of coercing silently" do
      System.put_env("INGRESS_PUBLISH_WIRE", "atmoic")

      assert {config, warning} = read_runtime()
      assert warning =~ "INGRESS_PUBLISH_WIRE"
      assert warning =~ "atmoic"
      assert warning =~ "atomic"
      assert warning =~ "single"
      assert config[:ingress][:publish_wire] == :atomic
    end

    test "an empty value is unrecognized, not the default" do
      System.put_env("INGRESS_PUBLISH_WIRE", "")

      assert {config, warning} = read_runtime()
      assert warning =~ "INGRESS_PUBLISH_WIRE"
      assert config[:ingress][:publish_wire] == :atomic
    end
  end

  describe "atomic-batch budgets" do
    test "the in-flight window and the broker hold ship as the accessors expect" do
      assert {config, ""} = read_runtime()
      assert config[:ingress][:publish_batch_inflight] == Ingress.Config.Publish.batch_inflight()
      assert config[:ingress][:publish_batch_hold_ms] == Ingress.Config.Publish.batch_hold_ms()
    end

    test "both budgets are operator-overridable for a rollback" do
      System.put_env("INGRESS_PUBLISH_BATCH_INFLIGHT", "4")
      System.put_env("INGRESS_PUBLISH_BATCH_HOLD_MS", "2000")

      assert {config, ""} = read_runtime()
      assert config[:ingress][:publish_batch_inflight] == 4
      assert config[:ingress][:publish_batch_hold_ms] == 2_000
    end
  end

  setup do
    keys = [
      "INGRESS_PUBLISH_WIRE",
      "INGRESS_PUBLISH_BATCH_INFLIGHT",
      "INGRESS_PUBLISH_BATCH_HOLD_MS"
    ]

    Enum.each(keys, &System.delete_env/1)
    on_exit(fn -> Enum.each(keys, &System.delete_env/1) end)
    :ok
  end

  defp read_runtime, do: with_io(:stderr, fn -> Config.Reader.read!(@runtime, env: :test) end)
end
