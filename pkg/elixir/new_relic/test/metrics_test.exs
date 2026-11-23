# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.MetricsTest do
  use ExUnit.Case, async: false

  defmodule Flaky do
    def increment(metric, value) do
      case :persistent_term.get(:metrics_test_mode) do
        :fail ->
          {:error, :unavailable}

        :ok ->
          send(Process.whereis(:metrics_test_sink), {:reported, metric, value})
          :ok
      end
    end
  end

  setup do
    Process.register(self(), :metrics_test_sink)
    :persistent_term.put(:metrics_test_mode, :ok)
    on_exit(fn -> :persistent_term.erase(:metrics_test_mode) end)
    :ok
  end

  defp start(name, reporter \\ Flaky) do
    start_supervised!(
      {Platform.Metrics, name: name, prefix: "Custom/Test/", flush_ms: 60_000, reporter: reporter}
    )
  end

  test "flush reports the prefixed delta" do
    start(:metrics_flush_test)
    Platform.Metrics.count(:metrics_flush_test, "A", 3)
    Platform.Metrics.count(:metrics_flush_test, "A")
    assert :ok = Platform.Metrics.flush(:metrics_flush_test)
    assert_received {:reported, "Custom/Test/A", 4}
  end

  test "flush resets the counter and reports nothing the second time" do
    start(:metrics_reset_test)
    Platform.Metrics.count(:metrics_reset_test, "A", 4)
    Platform.Metrics.flush(:metrics_reset_test)
    assert_received {:reported, "Custom/Test/A", 4}
    Platform.Metrics.flush(:metrics_reset_test)
    refute_received {:reported, _, _}
  end

  test "a failed report is retained and exported on the next good flush" do
    start(:metrics_failed_test)
    :persistent_term.put(:metrics_test_mode, :fail)
    Platform.Metrics.count(:metrics_failed_test, "B", 10)
    Platform.Metrics.flush(:metrics_failed_test)
    assert %{"B" => 10} = Platform.Metrics.snapshot(:metrics_failed_test)
    :persistent_term.put(:metrics_test_mode, :ok)
    Platform.Metrics.flush(:metrics_failed_test)
    assert_received {:reported, "Custom/Test/B", 10}
  end

  test "reporter may be a zero-arity function resolved per flush" do
    start(:metrics_fun_test, fn -> Flaky end)
    Platform.Metrics.count(:metrics_fun_test, "D", 2)
    Platform.Metrics.flush(:metrics_fun_test)
    assert_received {:reported, "Custom/Test/D", 2}
  end

  test "counting into a missing table is a no-op" do
    assert :ok = Platform.Metrics.count(:metrics_missing_test, "C", 5)
  end
end
