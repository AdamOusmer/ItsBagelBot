# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardDistribution do
  @behaviour Horde.DistributionStrategy

  @impl true
  def choose_node(child_spec, members) do
    members = Enum.reject(members, &draining?/1)

    case shard_id(child_spec) do
      nil ->
        Horde.UniformDistribution.choose_node(child_spec, members)

      shard_id ->
        alive =
          members
          |> Enum.filter(&match?(%{status: :alive}, &1))
          |> Enum.map(fn %{name: {_sup, node}} -> node end)

        case target_node(shard_id, alive) do
          nil -> {:error, :no_alive_nodes}
          target -> {:ok, Enum.find(members, fn %{name: {_sup, node}} -> node == target end)}
        end
    end
  end

  @impl true
  def has_quorum?(_members), do: true

  def target_node(_shard_id, []), do: nil

  def target_node(shard_id, nodes) do
    nodes = nodes |> Enum.sort() |> List.to_tuple()
    elem(nodes, rem(shard_id, tuple_size(nodes)))
  end

  def rebalance_candidate(placements, nodes),
    do: rebalance_candidate(placements, nodes, &node/1)

  @doc false
  def rebalance_candidate(placements, nodes, owner_node) do
    nodes = Enum.sort(nodes)
    counts = Enum.frequencies_by(placements, fn {_shard_id, owner} -> owner_node.(owner) end)

    if balanced?(nodes, counts) do
      nil
    else
      placements
      |> Enum.map(fn {shard_id, owner} ->
        source = owner_node.(owner)
        target = target_node(shard_id, nodes)
        {shard_id, owner, target, Map.get(counts, source, 0), Map.get(counts, target, 0)}
      end)
      |> Enum.filter(fn {_shard_id, _owner, target, source_load, target_load} ->
        target != nil and source_load > target_load
      end)
      |> Enum.sort_by(fn {shard_id, _owner, _target, source_load, target_load} ->
        {target_load, -source_load, shard_id}
      end)
      |> List.first()
      |> case do
        nil -> nil
        {shard_id, owner, target, _source_load, _target_load} -> {shard_id, owner, target}
      end
    end
  end

  defp balanced?([], _counts), do: true

  defp balanced?(nodes, counts) do
    loads = Enum.map(nodes, &Map.get(counts, &1, 0))
    Enum.max(loads) - Enum.min(loads) <= 1
  end

  def draining_node?(node) do
    case Horde.Registry.lookup(Ingress.Registry, {:draining, node}) do
      [{pid, _}] -> node(pid) in [node() | Node.list()]
      [] -> false
    end
  rescue
    ArgumentError -> false
  end

  defp draining?(%{name: {_sup, node}}), do: draining_node?(node)

  defp shard_id(%{start: {Ingress.ShardSession, :start_link, [opts]}}) when is_list(opts),
    do: Keyword.get(opts, :shard_id)

  defp shard_id(_child_spec), do: nil
end
