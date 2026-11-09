# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Valkey.ManagedRecords do
  @moduledoc "Atomic, generation-fenced record collections using a validated caller policy."
  alias Platform.Valkey.ManagedRecords.Policy

  @add_script """
  local p = cjson.decode(ARGV[#ARGV])
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 1 then return 'duplicate' end
  if redis.call('SCARD', KEYS[1]) >= tonumber(ARGV[3]) then return 'full' end
  local generation = redis.call('INCR', KEYS[3])
  redis.call('SADD', KEYS[1], ARGV[1])
  redis.call('ZREM', KEYS[4], ARGV[1])
  local key = ARGV[4] .. ARGV[1]
  for field, value in pairs(p.initial) do redis.call('HSET', key, field, value) end
  for _, field in ipairs(p.clear_on_add) do redis.call('HDEL', key, field) end
  redis.call('HSET', key, p.fields.generation, generation, p.fields.slot, ARGV[2])
  redis.call('INCR', KEYS[2])
  return tostring(generation)
  """
  @move_script """
  local p = cjson.decode(ARGV[#ARGV])
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 0 end
  local key = ARGV[5] .. ARGV[1]
  if redis.call('HGET', key, p.fields.generation) ~= ARGV[2] then return 0 end
  if redis.call('HGET', key, p.fields.enabled) == '0' then return 0 end
  local state = redis.call('HGET', key, p.fields.state)
  if state == p.states.stopping or state == p.states.disabled then return 0 end
  redis.call('HSET', key, p.fields.slot, ARGV[3], p.fields.moved_at, ARGV[4])
  redis.call('INCR', KEYS[2])
  return 1
  """
  @load_script """
  local p = cjson.decode(ARGV[#ARGV])
  if redis.call('HGET', KEYS[1], p.fields.generation) ~= ARGV[1] then return 0 end
  if (redis.call('HGET', KEYS[1], p.fields.slot) or '0') ~= ARGV[2] then return 0 end
  redis.call('HSET', KEYS[1], p.fields.load, ARGV[3])
  return 1
  """
  @stop_script """
  local p = cjson.decode(ARGV[#ARGV])
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 'absent' end
  local key = ARGV[3] .. ARGV[1]
  local current = redis.call('HGET', key, p.fields.stop_reason)
  local stopping = redis.call('HGET', key, p.fields.state) == p.states.stopping
  local sticky = false
  for _, reason in ipairs(p.sticky_reasons) do if current == reason then sticky = true end end
  local replace = not current or (current ~= ARGV[2] and not sticky)
  if stopping and not replace then return 'unchanged' end
  redis.call('HSET', key, p.fields.state, p.states.stopping)
  if replace then redis.call('HSET', key, p.fields.stop_reason, ARGV[2]) end
  redis.call('INCR', KEYS[2])
  return 'changed'
  """
  @set_enabled_script """
  local p = cjson.decode(ARGV[#ARGV])
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 'absent' end
  local key = ARGV[3] .. ARGV[1]
  if redis.call('HGET', key, p.fields.state) == p.states.stopping then return p.states.stopping end
  local current = redis.call('HGET', key, p.fields.enabled) or '1'
  if current == ARGV[2] then return redis.call('HGET', key, p.fields.state) or p.states.pending end
  local state = p.states.disabled
  if ARGV[2] == '1' and not redis.call('HGET', key, p.fields.subscription_id) then
    state = p.states.pending
  end
  redis.call('HSET', key, p.fields.enabled, ARGV[2], p.fields.state, state)
  redis.call('HDEL', key, p.fields.error)
  redis.call('INCR', KEYS[2])
  return state
  """
  @admit_script """
  if redis.call('SISMEMBER', KEYS[1], ARGV[1]) == 0 then return 'inactive' end
  if redis.call('HGET', KEYS[2], ARGV[3]) ~= ARGV[2] then return 'inactive' end
  if redis.call('HGET', KEYS[2], ARGV[4]) == '0' then return 'inactive' end
  if redis.call('HGET', KEYS[2], ARGV[5]) ~= ARGV[6] then return 'inactive' end
  if not redis.call('SET', KEYS[3], '1', 'NX', 'EX', ARGV[8]) then return 'duplicate' end
  redis.call('HINCRBY', KEYS[2], ARGV[7], 1)
  return 'first'
  """
  @fail_script """
  local p = cjson.decode(ARGV[#ARGV])
  if redis.call('HGET', KEYS[1], p.fields.generation) ~= ARGV[1] then return 0 end
  if redis.call('HGET', KEYS[1], p.fields.enabled) == '0' then return 0 end
  local state = redis.call('HGET', KEYS[1], p.fields.state)
  if state == p.states.stopping or state == p.states.disabled then return 0 end
  redis.call('HSET', KEYS[1], p.fields.state, p.states.failed, p.fields.error, ARGV[2])
  return 1
  """
  @finish_script """
  local p = cjson.decode(ARGV[#ARGV])
  local key = ARGV[3] .. ARGV[1]
  if redis.call('HGET', key, p.fields.state) ~= p.states.stopping then return 0 end
  if redis.call('HGET', key, p.fields.subscription_id) then return 0 end
  local state = p.states.removed
  if redis.call('HGET', key, p.fields.stop_reason) == p.promote_reason then state = p.states.promoted end
  redis.call('SREM', KEYS[1], ARGV[1])
  redis.call('INCR', KEYS[2])
  redis.call('HSET', key, p.fields.state, state)
  redis.call('EXPIRE', key, p.history_seconds)
  redis.call('ZADD', KEYS[3], ARGV[2], ARGV[1])
  return 1
  """
  @renew_script """
  local p = cjson.decode(ARGV[#ARGV])
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('PEXPIRE', KEYS[1], p.lease_ms)
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('PEXPIRE', KEYS[2], p.lease_ms)
    end
    return 1
  """
  @owner_session_script """
  local p = cjson.decode(ARGV[#ARGV])
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('SET', KEYS[2], ARGV[2] .. ':' .. ARGV[3], 'PX', p.lease_ms)
    return 1
  """
  @clear_owner_session_script """
  local p = cjson.decode(ARGV[#ARGV])
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('DEL', KEYS[2])
    end
    return 1
  """
  @release_script """
  local p = cjson.decode(ARGV[#ARGV])
    if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
    redis.call('DEL', KEYS[1])
    local session = redis.call('GET', KEYS[2]) or ''
    if string.sub(session, 1, string.len(ARGV[2]) + 1) == ARGV[2] .. ':' then
      redis.call('DEL', KEYS[2])
    end
    return 1
  """
  @display_name_script """
  local p = cjson.decode(ARGV[#ARGV])
  if redis.call('HGET', KEYS[1], p.fields.generation) ~= ARGV[1] then return 0 end
  redis.call('HSET', KEYS[1], p.fields.display_name, ARGV[2])
  return 1
  """

  @add_digest :crypto.hash(:sha, @add_script) |> Base.encode16(case: :lower)
  @move_digest :crypto.hash(:sha, @move_script) |> Base.encode16(case: :lower)
  @load_digest :crypto.hash(:sha, @load_script) |> Base.encode16(case: :lower)
  @stop_digest :crypto.hash(:sha, @stop_script) |> Base.encode16(case: :lower)
  @set_enabled_digest :crypto.hash(:sha, @set_enabled_script) |> Base.encode16(case: :lower)
  @admit_digest :crypto.hash(:sha, @admit_script) |> Base.encode16(case: :lower)
  @fail_digest :crypto.hash(:sha, @fail_script) |> Base.encode16(case: :lower)
  @finish_digest :crypto.hash(:sha, @finish_script) |> Base.encode16(case: :lower)
  @renew_digest :crypto.hash(:sha, @renew_script) |> Base.encode16(case: :lower)
  @owner_session_digest :crypto.hash(:sha, @owner_session_script) |> Base.encode16(case: :lower)
  @clear_owner_session_digest :crypto.hash(:sha, @clear_owner_session_script)
                              |> Base.encode16(case: :lower)
  @release_digest :crypto.hash(:sha, @release_script) |> Base.encode16(case: :lower)
  @display_name_digest :crypto.hash(:sha, @display_name_script) |> Base.encode16(case: :lower)

  def run(connection, %Policy{} = policy, operation, {keys, arguments}) do
    with {:ok, payload} <- Policy.encode(policy),
         true <- valid_operation?(operation, keys, arguments),
         {:ok, script} <- script_for(operation) do
      extra = if operation == :admit, do: Policy.admission_args(policy), else: [payload]
      Platform.Valkey.execute(connection, script, keys, arguments ++ extra)
    else
      _ -> {:error, :invalid}
    end
  end

  def admit_batch(connection, %Policy{} = policy, operations)
      when is_list(operations) and length(operations) <= 256 do
    with {:ok, _} <- Policy.encode(policy),
         true <-
           Enum.all?(operations, fn
             {keys, args} -> valid_operation?(:admit, keys, args)
             _ -> false
           end) do
      extra = Policy.admission_args(policy)
      # Keep pipeline order even if SCRIPT FLUSH races the batch. Retrying only
      # NOSCRIPT entries could reorder duplicate admission relative to later entries.
      commands =
        Enum.map(operations, fn {keys, args} ->
          ["EVAL", @admit_script, length(keys) | keys ++ args ++ extra]
        end)

      Platform.Valkey.pipeline(connection, commands)
    else
      _ -> {:error, :invalid}
    end
  end

  def admit_batch(_, _, _), do: {:error, :invalid}

  defp valid_operation?(operation, keys, args),
    do: bounded?(keys, args) and arity?(operation, keys, args)

  defp bounded?(keys, args)
       when is_list(keys) and is_list(args) and length(keys) in 1..8 and length(args) <= 16,
       do: Enum.all?(keys ++ args, &bounded_value?/1)

  defp bounded?(_, _), do: false

  defp bounded_value?(value) when is_binary(value), do: byte_size(value) in 1..4096
  defp bounded_value?(value), do: is_integer(value)

  defp arity?(op, keys, args) do
    expected = %{
      add: {4, 4},
      move: {2, 5},
      load: {1, 3},
      stop: {2, 3},
      set_enabled: {2, 3},
      admit: {3, 2},
      fail: {1, 2},
      finish: {3, 3},
      renew: {2, 2},
      owner_session: {2, 3},
      clear_owner_session: {2, 2},
      release: {2, 2},
      display_name: {1, 2}
    }

    Map.get(expected, op) == {length(keys), length(args)}
  end

  defp script_for(op)
       when op in [
              :add,
              :move,
              :load,
              :stop,
              :set_enabled,
              :admit,
              :fail,
              :finish,
              :renew,
              :owner_session,
              :clear_owner_session,
              :release,
              :display_name
            ],
       do: {:ok, script(op)}

  defp script_for(_), do: {:error, :invalid}

  defp script(:add), do: {@add_digest, @add_script}
  defp script(:move), do: {@move_digest, @move_script}
  defp script(:load), do: {@load_digest, @load_script}
  defp script(:stop), do: {@stop_digest, @stop_script}
  defp script(:set_enabled), do: {@set_enabled_digest, @set_enabled_script}
  defp script(:admit), do: {@admit_digest, @admit_script}
  defp script(:fail), do: {@fail_digest, @fail_script}
  defp script(:finish), do: {@finish_digest, @finish_script}
  defp script(:renew), do: {@renew_digest, @renew_script}
  defp script(:owner_session), do: {@owner_session_digest, @owner_session_script}

  defp script(:clear_owner_session),
    do: {@clear_owner_session_digest, @clear_owner_session_script}

  defp script(:release), do: {@release_digest, @release_script}
  defp script(:display_name), do: {@display_name_digest, @display_name_script}
end
