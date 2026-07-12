defmodule Ingress.MetricsTest do
  use ExUnit.Case, async: true

  alias Ingress.Metrics

  test "metrics are harmless when the New Relic agent is unavailable" do
    assert Metrics.count("Test/Count", 12) == :ok
    assert Metrics.event("Test/Event", %{value: 12}) == :ok
  end

  test "hot-path counters aggregate in ETS before a flush" do
    start_supervised!({Metrics, flush_ms: 60_000})

    for _ <- 1..1_000, do: Metrics.count("Test/Batched")

    assert [{"Test/Batched", 1_000}] = :ets.lookup(Ingress.Metrics.Counters, "Test/Batched")
    assert Metrics.flush() == :ok
  end
end
