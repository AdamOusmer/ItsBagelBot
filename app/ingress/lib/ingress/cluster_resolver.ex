defmodule Ingress.ClusterResolver do
  @moduledoc """
  Error-tolerant resolver shim for the libcluster `Cluster.Strategy.Kubernetes.DNS`
  strategy.

  libcluster polls the headless service every 5s and, on ANY resolver error,
  treats the answer as an empty node list and then actively disconnects every
  peer BEAM node (`Cluster.Strategy.Kubernetes.DNS.load/1`). A single node-local
  CoreDNS hiccup therefore partitions the whole cluster, and the Horde CRDT merge
  on recovery produces duplicate `{:shard, id}` registrations that flap the Twitch
  conduit binding (last-PATCH-wins) and drop a shard to `websocket_disconnected`.

  Wired in as the strategy's `:resolver`, this shim remembers the last successful
  NON-EMPTY address set and, on a resolver error, replays it as a synthetic
  `{:ok, hostent}` so the strategy sees "nothing changed" (`removed = ∅`) instead
  of "everyone left". A genuine membership change is a SUCCESSFUL lookup returning
  a different set, which passes through untouched, so real scale-downs and pod
  removals are still honored.

  Safety invariant: this shim can only ever make the strategy CONNECT to an
  address that was real at some point, never DISCONNECT a peer because DNS failed.
  Connecting to an address whose pod has since died is a no-op (the dist handshake
  fails, so the node never joins `nodes()` and never becomes a Horde member).
  `net_ticktime` plus Horde's `:dead` handoff remain the path that retires a peer
  that genuinely left, and that path is driven only by successful, honored lookups.

  State lives in `:persistent_term` (the same primitive `Ingress.Config` uses for
  the boot-time hot path), written on change only: a `:persistent_term.put`
  triggers a global literal GC, so it must never run on every poll, only when the
  membership set actually differs. No supervised child or ETS table is needed; a
  read before the first success returns `:none` and falls through to the raw
  error, which the strategy maps to `[]` against an empty membership: still a
  no-op.
  """

  @key {__MODULE__, :addresses}

  @doc """
  Resolver entrypoint handed to `Cluster.Strategy.Kubernetes.DNS` via the
  topology `:resolver` config. `inner` defaults to the same lookup libcluster
  uses by default and is injectable for tests.
  """
  def resolve(name, inner \\ &default_lookup/1) do
    handle(inner.(name), name)
  end

  # Successful lookup with at least one address: remember it, pass it through.
  defp handle(
         {:ok, {:hostent, _fqdn, _aliases, :inet, _len, [_ | _] = addresses}} = result,
         _name
       ) do
    remember(addresses)
    result
  end

  # Successful lookup with zero addresses: a genuine, honored shrink. Do not cache
  # an empty set over a good one; pass it through so the strategy removes the
  # departed peers.
  defp handle({:ok, {:hostent, _fqdn, _aliases, :inet, _len, []}} = result, _name), do: result

  # Resolver error: replay the last good set if we have one, turning a mass
  # disconnect into a no-op.
  defp handle({:error, _reason} = result, name), do: substitute(result, name)

  # Any other shape (for example a non-:inet hostent): leave it untouched.
  defp handle(result, _name), do: result

  defp substitute(error_result, name) do
    case :persistent_term.get(@key, :none) do
      :none -> error_result
      addresses -> {:ok, {:hostent, name, [], :inet, 4, addresses}}
    end
  end

  # Write-on-change only: a persistent_term put triggers a global literal GC, so
  # it runs solely when the membership set actually differs from the stored one.
  defp remember(addresses) do
    if :persistent_term.get(@key, :none) != addresses do
      :persistent_term.put(@key, addresses)
    end
  end

  defp default_lookup(name), do: :inet_res.getbyname(name, :a)
end
