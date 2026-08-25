# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.ChatDistribution do
  @moduledoc """
  Horde distribution strategy that places chat sessions deterministically by
  hashing the channel id across the alive members, sorted by node name, so a
  fleet of N nodes spreads M watched channels ~evenly and the same channel
  always lands on the same node for a given membership.

  Twitch's ingress places shards round-robin by shard id because Twitch owns
  shard numbering; YouTube has no such assignment, so the stable
  `:erlang.phash2/1` hash of the ownership key plays that role.

  Nodes carrying a `{:draining, node}` marker (registered by `YtIngress.Drain`
  during a planned shutdown) are excluded from every placement — a handoff
  successor placed back on the dying pod would die seconds later. A marker
  whose owner is no longer a visible cluster member is ignored, so a stale
  entry can never fence a node out permanently.

  Anything without a chat session start shape (the singletons) falls back to
  `Horde.UniformDistribution` over the same filtered members.
  """

  @behaviour Horde.DistributionStrategy

  @impl true
  def choose_node(child_spec, members) do
    members = Enum.reject(members, &draining?/1)

    case hash_key(child_spec) do
      nil ->
        Horde.UniformDistribution.choose_node(child_spec, members)

      hash ->
        alive =
          members
          |> Enum.filter(&match?(%{status: :alive}, &1))
          |> Enum.map(fn %{name: {_sup, node}} -> node end)

        case target_node(hash, alive) do
          nil -> {:error, :no_alive_nodes}
          target -> {:ok, Enum.find(members, fn %{name: {_sup, node}} -> node == target end)}
        end
    end
  end

  @impl true
  def has_quorum?(_members), do: true

  @doc """
  Returns the deterministic node for an ownership-key hash under the supplied
  membership.
  """
  def target_node(_hash, []), do: nil

  def target_node(hash, nodes) do
    nodes = Enum.sort(nodes)
    Enum.at(nodes, rem(hash, length(nodes)))
  end

  defp hash_key(%{start: {YtIngress.ChatSession, :start_link, [opts]}}) when is_list(opts) do
    case Keyword.fetch(opts, :registry_key) do
      {:ok, key} -> :erlang.phash2(key)
      # registry_key is required; a malformed spec is not placeable.
      _ -> nil
    end
  end

  defp hash_key(_child_spec), do: nil

  def draining_node?(node) do
    case Horde.Registry.lookup(YtIngress.Registry, {:draining, node}) do
      [{pid, _}] -> node(pid) in [node() | Node.list()]
      [] -> false
    end
  rescue
    # Registry not running (placement asked before the tree is up): no
    # markers can exist either.
    ArgumentError -> false
  end

  defp draining?(%{name: {_sup, node}}), do: draining_node?(node)
end
