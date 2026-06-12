defmodule Ingress.ShardDistribution do
  @moduledoc """
  Horde distribution strategy that places shard sessions round-robin by
  shard id across the alive members, sorted by node name, so a 5-shard
  Conduit on two nodes always splits 3/2 instead of wherever the default
  hash ring happens to land (4/1 in practice).

  Placement is deterministic given the same membership, which matters for
  `process_redistribution: :active`: after a node joins or leaves, Horde
  re-evaluates `choose_node/2` and moves exactly the shards whose target
  changed, restoring balance with the minimum number of session teardowns.

  Anything that is not a shard session (the ConduitManager singleton) falls
  back to `Horde.UniformDistribution`.
  """

  @behaviour Horde.DistributionStrategy

  @impl true
  def choose_node(child_spec, members) do
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

  defp shard_id(%{start: {Ingress.ShardSession, :start_link, [opts]}}) when is_list(opts),
    do: Keyword.get(opts, :shard_id)

  defp shard_id(_child_spec), do: nil
end
