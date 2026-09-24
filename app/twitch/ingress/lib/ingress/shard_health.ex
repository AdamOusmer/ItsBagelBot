# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardHealth do
  @rebind_grace_ms 60_000

  @max_unhealthy_observations 2

  def unhealthy_ids(shards, desired) when is_list(shards) do
    for %{"id" => id, "status" => status} <- shards,
        {int_id, ""} <- [Integer.parse(to_string(id))],
        int_id < desired and status != "enabled",
        do: int_id
  end

  def heal_action(probe, seen, now \\ DateTime.utc_now())
  def heal_action(:unreachable, _seen, _now), do: :restart

  def heal_action(_probe, seen, _now) when seen >= @max_unhealthy_observations, do: :restart

  def heal_action(%{bound: false}, _seen, _now), do: :skip

  def heal_action(%{bound: true, bound_at: bound_at}, _seen, now) do
    if settled?(bound_at, now), do: :force_rebind, else: :skip
  end

  def escalate?(seen), do: seen >= @max_unhealthy_observations

  def startable_ids(shards, desired) when is_list(shards) do
    enabled =
      for %{"id" => id, "status" => "enabled"} <- shards,
          {int_id, ""} <- [Integer.parse(to_string(id))],
          into: MapSet.new(),
          do: int_id

    Enum.reject(0..(desired - 1), &(&1 in enabled))
  end

  def unmanaged_action(%{name_state: name_state}, _desired) when name_state != :named, do: :ignore
  def unmanaged_action(%{shard_id: shard_id}, desired) when shard_id >= desired, do: :stop
  def unmanaged_action(_status, _desired), do: :ignore

  defp settled?(%DateTime{} = bound_at, now),
    do: DateTime.diff(now, bound_at, :millisecond) >= @rebind_grace_ms

  defp settled?(_bound_at, _now), do: true
end
