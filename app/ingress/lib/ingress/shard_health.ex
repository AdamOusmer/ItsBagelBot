defmodule Ingress.ShardHealth do
  @moduledoc """
  Pure decision logic for conduit shard self-healing.

  Twitch is the source of truth for whether a shard slot receives events: a
  conduit hash-routes each subscription to a fixed shard and never rebalances
  it to a healthy one, so a slot whose transport is not `"enabled"` drops its
  notifications silently while every local process reports healthy. The
  reconciler polls Twitch's per-shard status and repairs any slot that stays
  unhealthy. This module holds the decisions; `Ingress.ConduitManager`
  executes them.
  """

  # A shard bound more recently than this is still settling: Twitch's status
  # read may predate the bind, so healing would tear down a healthy socket.
  @rebind_grace_ms 60_000

  # A shard observed unhealthy this many consecutive reconcile ticks gets its
  # local session replaced outright, whatever state the session claims. This
  # is the backstop for sessions wedged in a state their own machinery never
  # leaves (a takeover that never completes, a lost reconnect timer). Two
  # ticks: one tick of unhealth is the normal shadow of an in-flight
  # reconnect (a healthy rebind lands well inside one tick interval); two
  # consecutive ticks means nobody is fixing it.
  @max_unhealthy_observations 2

  @doc """
  Shard ids (below `desired`) whose Twitch-side transport is not enabled.
  Slots at or above `desired` are scale-down leftovers the converge pass
  already stops; they are not health problems.
  """
  def unhealthy_ids(shards, desired) when is_list(shards) do
    for %{"id" => id, "status" => status} <- shards,
        {int_id, ""} <- [Integer.parse(to_string(id))],
        int_id < desired and status != "enabled",
        do: int_id
  end

  @doc """
  What to do about one Twitch-unhealthy shard, given the local session probe
  and how many consecutive reconcile ticks (`seen`) the shard has now been
  observed unhealthy.

    * `:restart` — no live process answers for the shard, or it has stayed
      unhealthy past the observation backstop; replace it.
    * `:force_rebind` — a session claims a settled binding Twitch says is
      dead (its socket may still receive keepalives after the binding moved
      or died, so the session cannot notice on its own); tear the socket
      down and bind a fresh session.
    * `:skip` — the session is mid-connect/bind; its own machinery heals.
  """
  def heal_action(probe, seen, now \\ DateTime.utc_now())
  def heal_action(:unreachable, _seen, _now), do: :restart

  def heal_action(_probe, seen, _now) when seen >= @max_unhealthy_observations, do: :restart

  def heal_action(%{bound: false}, _seen, _now), do: :skip

  def heal_action(%{bound: true, bound_at: bound_at}, _seen, now) do
    if settled?(bound_at, now), do: :force_rebind, else: :skip
  end

  @doc """
  True once `seen` consecutive unhealthy observations exhaust the session's
  chance to heal itself; callers escalate (replace the session, or bring up
  a rescue when no session can even be started).
  """
  def escalate?(seen), do: seen >= @max_unhealthy_observations

  defp settled?(%DateTime{} = bound_at, now),
    do: DateTime.diff(now, bound_at, :millisecond) >= @rebind_grace_ms

  defp settled?(_bound_at, _now), do: true
end
