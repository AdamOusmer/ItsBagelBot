defmodule Ingress.MetricsTest do
  use ExUnit.Case, async: true

  alias Ingress.Metrics

  test "metrics are harmless when the New Relic agent is unavailable" do
    assert Metrics.count("Test/Count", 12) == :ok
    assert Metrics.event("Test/Event", %{value: 12}) == :ok
  end
end
