# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.MetricsTest do
  use ExUnit.Case, async: false

  test "counter increments are visible in the shared snapshot" do
    ControlsGateway.Metrics.count("probe/count", 2)
    ControlsGateway.Metrics.count("probe/count")

    assert %{"probe/count" => 3} = Platform.Metrics.snapshot(ControlsGateway.Metrics)
  end

  test "a flush reports counters under the gateway prefix" do
    Process.register(self(), :metrics_probe)
    ControlsGateway.Metrics.count("probe/flush", 4)

    assert :ok = Platform.Metrics.flush(ControlsGateway.Metrics)
    assert_received {:metric, "Custom/ControlsGateway/probe/flush", 4}
  end
end
