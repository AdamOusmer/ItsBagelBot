# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.PublisherPoolFailbackTest do
  use ExUnit.Case, async: false

  alias Ingress.Nats.PublisherPool

  setup do
    on_exit(fn -> :persistent_term.erase({Ingress.Nats.Publisher, :n}) end)
  end

  for {label, shards, names} <- [
        {"a single publisher shard stays in place", 1, [:gnat_bus]},
        {"every publisher shard is covered when the pool can spill", 3,
         [:gnat_bus, :gnat_bus_pub_0, :gnat_bus_pub_1, :gnat_bus_pub_2]}
      ] do
    test label do
      :persistent_term.put({Ingress.Nats.Publisher, :n}, unquote(shards))
      assert unquote(names) == Enum.map(PublisherPool.hub_failback_connections(), &elem(&1, 0))
    end
  end
end
