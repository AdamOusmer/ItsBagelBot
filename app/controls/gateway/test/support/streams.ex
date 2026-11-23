# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.Streams do
  @moduledoc "Provisions the overlay streams, consumers and media bucket on the test hub."

  alias ControlsGateway.Overlays.Tiers
  alias Gnat.Jetstream.API.{Consumer, Object, Stream}

  @ttl_ns 300_000_000_000

  def provision(ack_wait_ns) do
    for tier <- Tiers.all() do
      stream = Tiers.stream(tier)

      {:ok, _} =
        Stream.create(:gnat_jetstream, %Stream{
          name: stream,
          subjects: ["overlays.#{tier}.>"],
          retention: :workqueue,
          storage: :memory,
          max_age: @ttl_ns,
          duplicate_window: @ttl_ns
        })

      {:ok, _} =
        Consumer.create(:gnat_jetstream, %Consumer{
          stream_name: stream,
          durable_name: Tiers.durable(tier),
          ack_wait: ack_wait_ns,
          max_deliver: -1,
          max_ack_pending: -1
        })
    end

    {:ok, _} =
      Object.create_bucket(:gnat_jetstream, "OVERLAYS_MEDIA", storage: :file, ttl: @ttl_ns)

    :ok
  end

  def stored(tier \\ :premium) do
    {:ok, %{state: %{messages: messages}}} = Stream.info(:gnat_jetstream, Tiers.stream(tier))
    messages
  end
end
