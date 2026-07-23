defmodule Ingress.ShardDistribution do
  @moduledoc """
  Horde distribution strategy that places shard sessions round-robin by
  shard id across the alive members, sorted by node name, so a 5-shard
  Conduit on two nodes always splits 3/2 instead of wherever the default
  hash ring happens to land (4/1 in practice).

  Placement is deterministic given the same membership. New shards land in a
  balanced shape immediately; `Ingress.ConduitManager` uses the same target
  calculation for a delayed make-before-break rebalance after membership has
  settled following a rollout.

  Nodes carrying a `{:draining, node}` marker (registered by `Ingress.Drain`
  during a planned shutdown) are excluded from every placement — a handoff
  successor placed back on the dying pod would die seconds later. A marker
  whose owner is no longer a visible cluster member is ignored, so a stale
  entry can never fence a node out permanently.

  Anything that is not a shard session (the singletons, rescue sessions)
  falls back to `Horde.UniformDistribution` over the same filtered members.
  """

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

  @doc """
  Returns the deterministic node for a shard under the supplied membership.
  """
  def target_node(_shard_id, []), do: nil

  def target_node(shard_id, nodes) do
    nodes = Enum.sort(nodes)
    Enum.at(nodes, rem(shard_id, length(nodes)))
  end

  @doc """
  Picks one move that strictly improves an imbalanced fleet.

  Placements are `{shard_id, pid}` pairs. A balanced fleet (node counts differ
  by at most one) is left alone even when shard ids do not sit on their
  deterministic target, avoiding needless WebSocket rebinds.
  """
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

  # A fleet with no nodes needs no move. Otherwise it is balanced when the
  # per-node shard counts differ by at most one; empty placements collapse to
  # all-zero loads, which reads as balanced.
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
    # Registry not running (placement asked before the tree is up): no
    # markers can exist either.
    ArgumentError -> false
  end

  defp draining?(%{name: {_sup, node}}), do: draining_node?(node)

  defp shard_id(%{start: {Ingress.ShardSession, :start_link, [opts]}}) when is_list(opts),
    do: Keyword.get(opts, :shard_id)

  defp shard_id(_child_spec), do: nil
end
