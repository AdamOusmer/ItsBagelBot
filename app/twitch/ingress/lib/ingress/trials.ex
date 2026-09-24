# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Trials do
  alias Ingress.TrialConduit
  alias Ingress.TrialValkey, as: VK

  @max_channels 30

  @members "trial:desired"
  @revision "trial:revision"
  @generation "trial:generation"
  @history "trial:history"
  @socket_target "trial:socket_target"

  @add_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 1 then return 'duplicate' end
  if redis.call('SCARD', KEYS[1]) >= tonumber(ARGV[3]) then return 'full' end
  local generation = redis.call('INCR', KEYS[3])
  redis.call('SADD', KEYS[1], ARGV[1])
  redis.call('ZREM', KEYS[4], ARGV[1])
  redis.call('HSET', 'trial:channel:' .. ARGV[1], 'generation', generation, 'state', 'pending', 'enabled', 1, 'received', 0, 'decoded', 0, 'processed', 0, 'failed', 0, 'retried', 0, 'blocked', 0, 'latency_samples', 0, 'latency_total_ms', 0)
  redis.call('HSET', 'trial:channel:' .. ARGV[1], 'slot', ARGV[2])
  redis.call('HDEL', 'trial:channel:' .. ARGV[1], 'error', 'stop_reason', 'display_name')
  redis.call('INCR', KEYS[2])
  return tostring(generation)
  """

  @move_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 0 end
  local key = 'trial:channel:' .. ARGV[1]
  if redis.call('HGET', key, 'generation') ~= ARGV[2] then return 0 end
  if redis.call('HGET', key, 'enabled') == '0' then return 0 end
  local state = redis.call('HGET', key, 'state')
  if state == 'stopping' or state == 'disabled' then return 0 end
  redis.call('HSET', key, 'slot', ARGV[3], 'moved_at', ARGV[4])
  redis.call('INCR', KEYS[2])
  return 1
  """

  @load_script """
  if redis.call('HGET', KEYS[1], 'generation') ~= ARGV[1] then return 0 end
  if (redis.call('HGET', KEYS[1], 'slot') or '0') ~= ARGV[2] then return 0 end
  redis.call('HSET', KEYS[1], 'load', ARGV[3])
  return 1
  """

  @stop_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 'absent' end
  redis.call('HSET', 'trial:channel:' .. ARGV[1], 'state', 'stopping')
  redis.call('INCR', KEYS[2])
  return 'stopping'
  """

  @set_enabled_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 'absent' end
  local key = 'trial:channel:' .. ARGV[1]
  if redis.call('HGET', key, 'state') == 'stopping' then return 'stopping' end
  local current = redis.call('HGET', key, 'enabled') or '1'
  if current == ARGV[2] then return redis.call('HGET', key, 'state') or 'pending' end
  local state = 'disabled'
  if ARGV[2] == '1' and not redis.call('HGET', key, 'subscription_id') then
    state = 'pending'
  end
  redis.call('HSET', key, 'enabled', ARGV[2], 'state', state)
  redis.call('HDEL', key, 'error')
  redis.call('INCR', KEYS[2])
  return state
  """

  @admit_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 'inactive' end
  if redis.call('HGET', KEYS[2], 'generation') ~= ARGV[2] then return 'inactive' end
  if redis.call('HGET', KEYS[2], 'enabled') == '0' then return 'inactive' end
  if redis.call('HGET', KEYS[2], 'state') ~= 'receiving' then return 'inactive' end
  if not redis.call('SET', KEYS[3], '1', 'NX', 'EX', 120) then return 'duplicate' end
  redis.call('HINCRBY', KEYS[2], 'received', 1)
  return 'first'
  """

  @fail_script """
  if redis.call('HGET', KEYS[1], 'generation') ~= ARGV[1] then return 0 end
  if redis.call('HGET', KEYS[1], 'enabled') == '0' then return 0 end
  local state = redis.call('HGET', KEYS[1], 'state')
  if state == 'stopping' or state == 'disabled' then return 0 end
  redis.call('HSET', KEYS[1], 'state', 'failed', 'error', ARGV[2])
  return 1
  """

  @finish_script """
  local key = 'trial:channel:' .. ARGV[1]
  if redis.call('HGET', key, 'state') ~= 'stopping' then return 0 end
  if redis.call('HGET', key, 'subscription_id') then return 0 end
  redis.call('SREM', KEYS[1], ARGV[1])
  redis.call('INCR', KEYS[2])
  redis.call('HSET', key, 'state', ARGV[2])
  redis.call('EXPIRE', key, 604800)
  redis.call('ZADD', KEYS[3], ARGV[3], ARGV[1])
  return 1
  """

  def valid_id?(id) when is_binary(id) do
    String.match?(id, ~r/^[1-9][0-9]{0,19}$/) &&
      String.to_integer(id) <= 18_446_744_073_709_551_615
  end

  def valid_id?(_), do: false

  def max_channels, do: @max_channels

  def add(id) do
    with true <- valid_id?(id),
         {:ok, %{trials: rows, socket_target: target}} <- list(),
         slot = TrialConduit.slot_for_new(rows, target),
         {:ok, value} <-
           VK.command([
             "EVAL",
             @add_script,
             4,
             @members,
             @revision,
             @generation,
             @history,
             id,
             slot,
             @max_channels
           ]) do
      case value do
        "full" -> {:error, "full"}
        "duplicate" -> {:ok, "duplicate"}
        generation -> {:ok, generation}
      end
    else
      false -> {:error, "invalid_id"}
      {:error, _} -> {:error, "unavailable"}
    end
  end

  def move(id, generation, slot) do
    VK.command([
      "EVAL",
      @move_script,
      2,
      @members,
      @revision,
      id,
      generation,
      slot,
      System.system_time(:second)
    ])
  end

  def set_socket_target(target), do: VK.command(["SET", @socket_target, target])

  def record_load(id, generation, slot, load),
    do: VK.command(["EVAL", @load_script, 1, "trial:channel:" <> id, generation, slot, load])

  def stop(id) do
    case VK.command(["EVAL", @stop_script, 2, @members, @revision, id]) do
      {:ok, state} -> {:ok, state}
      {:error, _} -> {:error, "unavailable"}
    end
  end

  def set_enabled(id, enabled) when is_boolean(enabled) do
    value = if enabled, do: "1", else: "0"

    case VK.command(["EVAL", @set_enabled_script, 2, @members, @revision, id, value]) do
      {:ok, "absent"} -> {:error, "not_found"}
      {:ok, "stopping"} -> {:error, "stopping"}
      {:ok, state} -> {:ok, state}
      {:error, _} -> {:error, "unavailable"}
    end
  end

  def finish(id, state) when state in ["removed", "promoted"] do
    VK.command([
      "EVAL",
      @finish_script,
      3,
      @members,
      @revision,
      @history,
      id,
      state,
      System.system_time(:second)
    ])
  end

  def list do
    cutoff = System.system_time(:second) - 604_800

    with {:ok, ids} <- VK.command(["SMEMBERS", @members]),
         {:ok, _} <- VK.command(["ZREMRANGEBYSCORE", @history, "-inf", cutoff]),
         {:ok, history_ids} <- VK.command(["ZRANGE", @history, 0, -1]),
         {:ok, revision} <- VK.command(["GET", @revision]),
         {:ok, target} <- VK.command(["GET", @socket_target]),
         {:ok, rows} <- rows(Enum.uniq(ids ++ history_ids)) do
      {:ok,
       %{
         version: String.to_integer(revision || "0"),
         active_count: length(ids),
         max_channels: @max_channels,
         socket_target: TrialConduit.clamp(String.to_integer(target || "1")),
         trials: rows
       }}
    end
  end

  defp rows(ids) do
    Enum.reduce_while(ids, {:ok, []}, fn id, {:ok, acc} ->
      case VK.command(["HGETALL", "trial:channel:" <> id]) do
        {:ok, fields} ->
          row = fields |> Enum.chunk_every(2) |> Map.new(fn [k, v] -> {k, v} end)

          {:cont,
           {:ok,
            [
              %{
                broadcaster_id: id,
                state: row["state"] || "pending",
                enabled: row["enabled"] != "0",
                slot: slot(row["slot"]),
                load: integer(row["load"]),
                moved_at: integer(row["moved_at"]),
                error: row["error"],
                generation: row["generation"],
                display_name: row["display_name"],
                subscription_id: row["subscription_id"],
                session_id: row["session_id"],
                stop_reason: row["stop_reason"],
                received: integer(row["received"]),
                decoded: integer(row["decoded"]),
                processed: integer(row["processed"]),
                failed: integer(row["failed"]),
                retried: integer(row["retried"]),
                blocked_actions: integer(row["blocked"]),
                average_processing_latency_ms: average_latency(row)
              }
              | acc
            ]}}

        error ->
          {:halt, error}
      end
    end)
    |> case do
      {:ok, rows} -> {:ok, Enum.reverse(rows)}
      error -> error
    end
  end

  defp slot(nil), do: 0
  defp slot(value), do: String.to_integer(value)

  defp integer(nil), do: 0
  defp integer(value), do: String.to_integer(value)

  defp average_latency(row) do
    samples = integer(row["latency_samples"])
    if samples == 0, do: 0, else: div(integer(row["latency_total_ms"]), samples)
  end

  def field(id, name, value), do: VK.command(["HSET", "trial:channel:" <> id, name, value])

  def fail(id, generation, reason),
    do: VK.command(["EVAL", @fail_script, 1, "trial:channel:" <> id, generation, reason])

  def display_name(id, generation, name) when is_binary(name) and name != "" do
    script = """
    if redis.call('HGET', KEYS[1], 'generation') ~= ARGV[1] then return 0 end
    redis.call('HSET', KEYS[1], 'display_name', ARGV[2])
    return 1
    """

    VK.command(["EVAL", script, 1, "trial:channel:" <> id, generation, name])
  end

  def increment(id, name), do: VK.command(["HINCRBY", "trial:channel:" <> id, name, 1])

  def admit(id, generation, chat_id) do
    case VK.command([
           "EVAL",
           @admit_script,
           3,
           @members,
           "trial:channel:" <> id,
           "trial:dedup:" <> id <> ":" <> chat_id,
           id,
           generation
         ]) do
      {:ok, "first"} -> :first
      {:ok, "duplicate"} -> :duplicate
      {:ok, "inactive"} -> :inactive
      _ -> :unavailable
    end
  end

  def owners do
    VK.command(["MGET" | Enum.map(TrialConduit.slots(), &lease_key/1)])
  end

  def acquire(owner, slot \\ 0) do
    with {:ok, epoch} <- VK.command(["INCR", "trial:owner_epoch"]),
         {:ok, result} <-
           VK.command(["SET", lease_key(slot), "#{epoch}:#{owner}", "NX", "PX", 60_000]) do
      if result == "OK", do: {:ok, epoch}, else: {:error, :owned}
    end
  end

  @renew_script """
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('PEXPIRE', KEYS[1], 60000)
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('PEXPIRE', KEYS[2], 60000)
    end
    return 1
  """

  def renew(owner, epoch, slot \\ 0), do: owner_script(@renew_script, owner, epoch, slot)

  @owner_session_script """
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('SET', KEYS[2], ARGV[2] .. ':' .. ARGV[3], 'PX', 60000)
    return 1
  """

  def owner_session(owner, epoch, session_id, slot \\ 0) when is_binary(session_id) do
    VK.command([
      "EVAL",
      @owner_session_script,
      2,
      lease_key(slot),
      session_key(slot),
      "#{epoch}:#{owner}",
      epoch,
      session_id
    ])
  end

  @clear_owner_session_script """
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('DEL', KEYS[2])
    end
    return 1
  """

  def clear_owner_session(owner, epoch, slot \\ 0),
    do: owner_script(@clear_owner_session_script, owner, epoch, slot)

  @release_script """
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('DEL', KEYS[1])
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('DEL', KEYS[2])
    end
    return 1
  """

  def release(owner, epoch, slot), do: owner_script(@release_script, owner, epoch, slot)

  defp owner_script(script, owner, epoch, slot) do
    VK.command([
      "EVAL",
      script,
      2,
      lease_key(slot),
      session_key(slot),
      "#{epoch}:#{owner}",
      epoch
    ])
  end

  # Slot 0 keeps the single-socket key names that outgress already reads.
  defp lease_key(0), do: "trial:owner"
  defp lease_key(slot), do: "trial:owner:#{slot}"
  defp session_key(0), do: "trial:owner_session"
  defp session_key(slot), do: "trial:owner_session:#{slot}"
end
