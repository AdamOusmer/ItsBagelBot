defmodule Ingress.ShardDistribution do
  @moduledoc """
  Horde distribution strategy that places shard sessions round-robin by
  shard id across the alive members, sorted by node name, so a 5-shard
  Conduit on two nodes always splits 3/2 instead of wherever the default
  hash ring happens to land (4/1 in practice).

  Placement is deterministic given the same membership, so restarts and
  scale-ups land in a balanced shape without any rebalancing moves.

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
        members
        |> Enum.filter(&match?(%{status: :alive}, &1))
        |> Enum.sort_by(fn %{name: {_sup, node}} -> node end)
        |> case do
          [] -> {:error, :no_alive_nodes}
          alive -> {:ok, Enum.at(alive, rem(shard_id, length(alive)))}
        end
    end
  end

  @impl true
  def has_quorum?(_members), do: true

  defp draining?(%{name: {_sup, node}}) do
    case Horde.Registry.lookup(Ingress.Registry, {:draining, node}) do
      [{pid, _}] -> node(pid) in [node() | Node.list()]
      [] -> false
    end
  rescue
    # Registry not running (placement asked before the tree is up): no
    # markers can exist either.
    ArgumentError -> false
  end

  defp shard_id(%{start: {Ingress.ShardSession, :start_link, [opts]}}) when is_list(opts),
    do: Keyword.get(opts, :shard_id)

  defp shard_id(_child_spec), do: nil
end
