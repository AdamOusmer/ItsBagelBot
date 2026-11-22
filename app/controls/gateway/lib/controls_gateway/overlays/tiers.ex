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

  def tier(name), do: Enum.find(all(), &(Atom.to_string(&1) == name))

  def ack_subject?("$JS.ACK." <> tokens), do: tokens |> String.split(".") |> ack_tokens?()
  def ack_subject?(_subject), do: false

  defp ack_tokens?([stream, durable | counters]) when length(counters) == 5,
    do: consumer?(stream, durable) and numeric?(counters)

  defp ack_tokens?([domain, hash, stream, durable | rest]) when length(rest) == 6 do
    {counters, [random]} = Enum.split(rest, 5)

    consumer?(stream, durable) and numeric?(counters) and
      Enum.all?([domain, hash, random], &token?/1)
  end

  defp ack_tokens?(_tokens), do: false

  defp consumer?(stream, durable),
    do: Enum.any?(all(), &({stream, durable} == {stream(&1), durable(&1)}))

  defp token?(token), do: Regex.match?(~r/\A[A-Za-z0-9_-]+\z/, token)

  defp numeric?(tokens), do: Enum.all?(tokens, &match?({_n, ""}, Integer.parse(&1)))
end
