# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Ingress.MetricsRetryTest do
  use ExUnit.Case, async: false

  defmodule Failing do
    def increment(_, _), do: {:error, :unavailable}
  end

  defmodule Succeeding do
    def increment(_, _), do: :ok
  end

  test "counter deltas survive failed export" do
    old = Application.get_env(:ingress, :metrics_reporter)

    on_exit(fn ->
      if old,
        do: Application.put_env(:ingress, :metrics_reporter, old),
        else: Application.delete_env(:ingress, :metrics_reporter)
    end)

    start_supervised!({Ingress.Metrics, flush_ms: 60_000})
    Application.put_env(:ingress, :metrics_reporter, Failing)
    assert :ok = Ingress.Metrics.count("Retry/Fixture", 10)
    assert :ok = Ingress.Metrics.flush()
    assert [{"Retry/Fixture", 10}] = :ets.lookup(Ingress.Metrics.Counters, "Retry/Fixture")
    Application.put_env(:ingress, :metrics_reporter, Succeeding)
    assert :ok = Ingress.Metrics.flush()
    assert [{"Retry/Fixture", 0}] = :ets.lookup(Ingress.Metrics.Counters, "Retry/Fixture")
  end
end
