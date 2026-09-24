# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialConduit do
  alias Ingress.Capacity
  alias Ingress.ShardScaler.Policy

  @min_sockets 1
  @max_sockets 3
  @settle_seconds 60

  def slots, do: Enum.to_list(0..(@max_sockets - 1))

  def budget do
    case Application.get_env(:ingress, :trial_socket_budget_eps) do
      eps when is_integer(eps) and eps > 0 -> eps * Capacity.load_window_seconds()
      _ -> Policy.budget_per_window()
    end
  end

  def clamp(target), do: target |> max(@min_sockets) |> min(@max_sockets)

  def socket_floor(rows), do: rows |> placed() |> length() |> clamp()

  def effective_target(rows, target), do: clamp(max(target, socket_floor(rows)))

  def slot_for_new(rows, target) do
    placed = placed(rows)
    sockets = clamp(max(target, length(placed) + 1))

    placed
    |> stats(sockets)
    |> Enum.min_by(fn {slot, s} -> {s.count, s.load, slot} end)
    |> elem(0)
  end

  def yield_slot?(owners, slot, node, cluster) do
    mine = held_slots(owners, node)

    length(mine) > 1 and slot == Enum.max(mine) and
      Enum.any?(cluster, &(held_slots(owners, &1) == []))
  end

  defp held_slots(owners, member) do
    for {owner, slot} <- Enum.with_index(owners), held_by?(owner, member), do: slot
  end

  defp held_by?(owner, member), do: is_binary(owner) and String.contains?(owner, inspect(member))

  def settling?(rows, now_seconds),
    do: Enum.any?(placed(rows), &(&1.moved_at > now_seconds - @settle_seconds))

  def scale(rows, target, ticks, budget \\ budget()) do
    target = effective_target(rows, target)

    per_socket =
      Enum.map(stats(placed(rows), target), fn {slot, s} ->
        {slot, {:ok, div(s.load * Policy.budget_per_window(), budget)}}
      end)

    sample = Policy.summarize_sample(target, per_socket)

    {target, ticks, _action} =
      Policy.evaluate(sample, target, ticks, socket_floor(rows), @max_sockets)

    {target, ticks}
  end

  def next_move(rows, target, now_seconds, budget \\ budget()) do
    placed = placed(rows)
    target = effective_target(placed, target)
    {inside, outside} = Enum.split_with(placed, &(&1.slot < target))

    cond do
      outside != [] -> {quietest(outside), coolest(inside, target)}
      settling?(placed, now_seconds) -> nil
      true -> relieve(inside, target, budget) || balance(inside, target, budget)
    end
  end

  defp balance(rows, target, budget) do
    stats = stats(rows, target)
    {full, from} = Enum.max_by(stats, fn {slot, s} -> {s.count, -slot} end)
    {empty, to} = Enum.min_by(stats, fn {slot, s} -> {s.count, slot} end)

    if from.count - to.count > 1 do
      row = quietest(Enum.filter(rows, &(&1.slot == full)))
      if to.load + row.load <= budget, do: {row, empty}
    end
  end

  defp relieve(rows, target, budget) do
    stats = stats(rows, target)
    {hot, %{load: high}} = Enum.max_by(stats, fn {slot, s} -> {s.load, -slot} end)
    cool = coolest(rows, target)
    gap = high - stats[cool].load
    room = budget - stats[cool].load

    candidates =
      Enum.filter(rows, &(&1.slot == hot and &1.load > 0 and &1.load < gap and &1.load <= room))

    if high >= budget and candidates != [],
      do: {Enum.min_by(candidates, &{abs(gap - 2 * &1.load), &1.load, &1.broadcaster_id}), cool}
  end

  defp placed(rows),
    do: Enum.filter(rows, &(&1.enabled and &1.state in ["pending", "receiving", "failed"]))

  defp coolest(rows, target) do
    rows
    |> stats(target)
    |> Enum.min_by(fn {slot, s} -> {s.load, s.count, slot} end)
    |> elem(0)
  end

  defp stats(rows, target) do
    base = Map.new(0..(clamp(target) - 1), &{&1, %{load: 0, count: 0}})

    Enum.reduce(rows, base, fn row, acc ->
      Map.replace_lazy(acc, row.slot, &%{load: &1.load + row.load, count: &1.count + 1})
    end)
  end

  defp quietest(rows), do: Enum.min_by(rows, &{&1.load, &1.received, &1.broadcaster_id})
end
