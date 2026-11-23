# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Tiers do
  @tiers [
    premium: {"OVERLAYS_PREMIUM", "overlays-premium"},
    standard: {"OVERLAYS_STANDARD", "overlays-standard"}
  ]

  def all, do: Keyword.keys(@tiers)

  def stream(tier), do: @tiers |> Keyword.fetch!(tier) |> elem(0)

  def durable(tier), do: @tiers |> Keyword.fetch!(tier) |> elem(1)

  def consumers, do: Keyword.values(@tiers)

  def puller(tier), do: Module.concat(ControlsGateway.Overlays.Pull, Macro.camelize("#{tier}"))

  def tier(name), do: Enum.find(all(), &(Atom.to_string(&1) == name))
end
