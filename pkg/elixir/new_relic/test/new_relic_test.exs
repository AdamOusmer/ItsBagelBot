# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.NewRelicTest do
  use ExUnit.Case, async: false

  test "without a license instrumentation stays disabled and preserves callback results" do
    assert Platform.NewRelic.enabled?() == false
    assert :ok = Platform.NewRelic.increment("Custom/Test", 1)
    assert :ok = Platform.NewRelic.event("Test", %{})
    assert [] = Platform.NewRelic.trace_headers()
    assert :ok = Platform.NewRelic.sample_trace()
    assert :result = Platform.NewRelic.transaction("Test", "Test", [], fn -> :result end)
    assert :result = Platform.NewRelic.span("Test", [], fn -> :result end)

    assert_raise RuntimeError, fn ->
      Platform.NewRelic.transaction("Test", "Test", [], fn -> raise "callback" end)
    end
  end

  test "continue runs the callback whatever the inbound trace headers carry" do
    sampled = [{"Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}]
    unsampled = [{"traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-00"}]

    for headers <- [sampled, unsampled, [], nil] do
      assert :result = Platform.NewRelic.continue("Test", "Test", headers, fn -> :result end)
    end

    assert_raise RuntimeError, fn ->
      Platform.NewRelic.continue("Test", "Test", sampled, fn -> raise "callback" end)
    end
  end

  test "runtime policy rejects SDK overrides that enable automatic capture" do
    previous = System.get_env("NEW_RELIC_LOGS_IN_CONTEXT")

    on_exit(fn ->
      if previous,
        do: System.put_env("NEW_RELIC_LOGS_IN_CONTEXT", previous),
        else: System.delete_env("NEW_RELIC_LOGS_IN_CONTEXT")
    end)

    System.put_env("NEW_RELIC_LOGS_IN_CONTEXT", "direct")
    assert_raise RuntimeError, fn -> Platform.NewRelic.runtime_config("fixture") end
    System.delete_env("NEW_RELIC_LOGS_IN_CONTEXT")
    assert Platform.NewRelic.runtime_config("fixture")[:logs_in_context] == :disabled
    System.put_env("NEW_RELIC_LOGS_IN_CONTEXT", "forwarder")
    assert_raise RuntimeError, fn -> Platform.NewRelic.runtime_config("fixture") end

    assert Platform.NewRelic.runtime_config("fixture", logs_in_context: :forwarder)[
             :logs_in_context
           ] ==
             :forwarder

    System.put_env("NEW_RELIC_LOGS_IN_CONTEXT", "direct")

    assert_raise RuntimeError, fn ->
      Platform.NewRelic.runtime_config("fixture", logs_in_context: :forwarder)
    end
  end

  test "error collection is off unless the service opts in" do
    assert Platform.NewRelic.runtime_config("fixture")[:error_collector_enabled] == false

    assert Platform.NewRelic.runtime_config("fixture", error_collector: true)[
             :error_collector_enabled
           ] == true

    assert_raise RuntimeError, fn ->
      Platform.NewRelic.runtime_config("fixture", error_collector: "true")
    end
  end
end
