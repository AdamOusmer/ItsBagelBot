# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Trials do
  @moduledoc "Durable trial admission and ownership over Valkey."

  alias Ingress.TrialValkey, as: VK

  @members "trial:desired"
  @revision "trial:revision"
  @generation "trial:generation"
  @lease "trial:owner"
  @owner_session "trial:owner_session"
  @history "trial:history"

  @add_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 1 then return 'duplicate' end
  if redis.call('SCARD', KEYS[1]) >= 4 then return 'full' end
  local generation = redis.call('INCR', KEYS[3])
  redis.call('SADD', KEYS[1], ARGV[1])
  redis.call('ZREM', KEYS[4], ARGV[1])
  redis.call('HSET', 'trial:channel:' .. ARGV[1], 'generation', generation, 'state', 'pending', 'received', 0, 'decoded', 0, 'processed', 0, 'failed', 0, 'retried', 0, 'blocked', 0, 'latency_samples', 0, 'latency_total_ms', 0)
  redis.call('HDEL', 'trial:channel:' .. ARGV[1], 'error', 'stop_reason', 'display_name')
  redis.call('INCR', KEYS[2])
  return tostring(generation)
  """

  @stop_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 'absent' end
  redis.call('HSET', 'trial:channel:' .. ARGV[1], 'state', 'stopping')
  redis.call('INCR', KEYS[2])
  return 'stopping'
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

  def add(id) do
    with true <- valid_id?(id),
         {:ok, value} <-
           VK.command(["EVAL", @add_script, 4, @members, @revision, @generation, @history, id]) do
      case value do
        "full" -> {:error, "full"}
        "duplicate" -> {:ok, "pending"}
        generation -> {:ok, generation}
      end
    else
      false -> {:error, "invalid_id"}
      {:error, _} -> {:error, "unavailable"}
    end
  end

  def stop(id) do
    case VK.command(["EVAL", @stop_script, 2, @members, @revision, id]) do
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
         {:ok, rows} <- rows(Enum.uniq(ids ++ history_ids)) do
      {:ok,
       %{version: String.to_integer(revision || "0"), active_count: length(ids), trials: rows}}
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

  defp integer(nil), do: 0
  defp integer(value), do: String.to_integer(value)

  defp average_latency(row) do
    samples = integer(row["latency_samples"])
    if samples == 0, do: 0, else: div(integer(row["latency_total_ms"]), samples)
  end

  def field(id, name, value), do: VK.command(["HSET", "trial:channel:" <> id, name, value])

  def display_name(id, generation, name) when is_binary(name) and name != "" do
    script = """
    if redis.call('HGET', KEYS[1], 'generation') ~= ARGV[1] then return 0 end
    redis.call('HSET', KEYS[1], 'display_name', ARGV[2])
    return 1
    """

    VK.command(["EVAL", script, 1, "trial:channel:" <> id, generation, name])
  end

  def increment(id, name), do: VK.command(["HINCRBY", "trial:channel:" <> id, name, 1])

  def dedup(id, chat_id) do
    case VK.command(["SET", "trial:dedup:" <> id <> ":" <> chat_id, "1", "NX", "EX", 120]) do
      {:ok, "OK"} -> :first
      {:ok, nil} -> :duplicate
      _ -> :unavailable
    end
  end

  def acquire(owner) do
    with {:ok, epoch} <- VK.command(["INCR", "trial:owner_epoch"]),
         {:ok, result} <- VK.command(["SET", @lease, "#{epoch}:#{owner}", "NX", "PX", 60_000]) do
      if result == "OK", do: {:ok, epoch}, else: {:error, :owned}
    end
  end

  def renew(owner, epoch) do
    script = """
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('PEXPIRE', KEYS[1], 60000)
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('PEXPIRE', KEYS[2], 60000)
    end
    return 1
    """

    VK.command(["EVAL", script, 2, @lease, @owner_session, "#{epoch}:#{owner}", epoch])
  end

  def owner_session(owner, epoch, session_id) when is_binary(session_id) do
    script = """
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('SET', KEYS[2], ARGV[2] .. ':' .. ARGV[3], 'PX', 60000)
    return 1
    """

    VK.command([
      "EVAL",
      script,
      2,
      @lease,
      @owner_session,
      "#{epoch}:#{owner}",
      epoch,
      session_id
    ])
  end

  def clear_owner_session(owner, epoch) do
    script = """
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('DEL', KEYS[2])
    end
    return 1
    """

    VK.command(["EVAL", script, 2, @lease, @owner_session, "#{epoch}:#{owner}", epoch])
  end
end
