# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.MetricsProbe do
  @moduledoc "Test reporter: forwards flushed counters to the process registered as :metrics_probe."

  def increment(name, value) do
    if pid = Process.whereis(:metrics_probe), do: send(pid, {:metric, name, value})
    :ok
  end
end
