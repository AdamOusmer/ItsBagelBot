# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Valkey do
  @moduledoc """
  Generic bounded Valkey connection and retained ordered stream primitives.
  Errors are `:unavailable` or `{:command, message}`; client structs never escape.
  """
  @timeout 2_000

  def child_spec(opts), do: Redix.child_spec(opts)

  def start_link(opts), do: Redix.start_link(opts)
  def get(conn, key), do: command(conn, ["GET", key])
  def set(conn, key, value), do: command(conn, ["SET", key, value])

  def set_if_absent_ms(conn, key, value, ms),
    do: command(conn, ["SET", key, value, "NX", "PX", ms])

  def increment(conn, key), do: command(conn, ["INCR", key])
  def hash_get_all(conn, key), do: command(conn, ["HGETALL", key])
  def hash_set(conn, key, field, value), do: command(conn, ["HSET", key, field, value])
  def hash_increment(conn, key, field, value), do: command(conn, ["HINCRBY", key, field, value])
  def set_members(conn, key), do: command(conn, ["SMEMBERS", key])

  def sorted_remove_before(conn, key, cutoff),
    do: command(conn, ["ZREMRANGEBYSCORE", key, "-inf", cutoff])

  def sorted_members(conn, key), do: command(conn, ["ZRANGE", key, 0, -1])
  def get_many(conn, keys), do: command(conn, ["MGET" | keys])

  def hash_increments(conn, items),
    do: pipeline(conn, Enum.map(items, fn {key, field, n} -> ["HINCRBY", key, field, n] end))

  def pipeline(conn, commands) do
    case Redix.pipeline(conn, commands, timeout: @timeout) do
      {:ok, results} -> {:ok, Enum.map(results, &entry/1)}
      {:error, reason} -> {:error, error(reason)}
    end
  catch
    :exit, _ -> {:error, :unavailable}
  end

  def ready?(conn) do
    Redix.command(conn, ["PING"], timeout: 200) == {:ok, "PONG"}
  catch
    :exit, _ -> false
  end

  def execute(conn, {digest, script}, keys, args) do
    count = length(keys)

    case command(conn, ["EVALSHA", digest, count | keys ++ args]) do
      {:error, {:command, "NOSCRIPT" <> _}} ->
        command(conn, ["EVAL", script, count | keys ++ args])

      result ->
        result
    end
  end

  defp command(conn, command) do
    case Redix.command(conn, command, timeout: @timeout) do
      {:ok, value} -> {:ok, value}
      {:error, reason} -> {:error, error(reason)}
    end
  catch
    :exit, _ -> {:error, :unavailable}
  end

  defp entry(%Redix.Error{} = reason), do: {:error, error(reason)}
  defp entry(value), do: value

  defp error(%Redix.Error{message: message}), do: {:command, message}
  defp error(_), do: :unavailable
end
