# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Singleton do
  @moduledoc """
  Facade over "call the cluster singleton registered under this name": the
  Horde registry lookup, then a `GenServer.call` guarded against the `:exit`
  that a dying, migrating or unreachable owner raises in the caller.

  Six call sites open-coded that lookup/try/catch, and the two silences it can
  end in are not the same silence: a name nobody holds means the singleton is
  down (a handover in flight, or a node that has not rejoined), while a name
  that will not answer means its owner is wedged. The admin and conduit
  endpoints report those as different states, so `call/4` hands the reason to
  the fallback rather than collapsing both into one value.

  The fallback is a function, not a term: `Ingress.ShardScaler` falls back to a
  freshly computed config-floor status, which must not be built on the path
  where the singleton does answer.
  """

  @registry Ingress.Registry

  @type name :: term()
  @type reason :: :down | {:unresponsive, pid()}

  @doc """
  Resolves the singleton's pid. `:error` means the name is unheld — registry
  lag included, so callers that own the name treat it as "mine or lagging"
  rather than as a verdict.
  """
  @spec lookup(name()) :: {:ok, pid()} | :error
  def lookup(name) do
    case Horde.Registry.lookup(@registry, name) do
      [{pid, _value}] -> {:ok, pid}
      _unheld -> :error
    end
  end

  @doc """
  Calls `pid`, converting the caller-side `:exit` of a timeout or a dead
  process into `:error`.
  """
  @spec safe_call(pid(), term(), timeout()) :: {:ok, term()} | :error
  def safe_call(pid, message, timeout) do
    {:ok, GenServer.call(pid, message, timeout)}
  catch
    :exit, _reason -> :error
  end

  @doc """
  Calls the singleton registered under `name`, or returns `fallback.(reason)`
  when it is down or does not answer within `timeout`.
  """
  @spec call(name(), term(), timeout(), (reason() -> result)) :: term() | result
        when result: term()
  def call(name, message, timeout, fallback) when is_function(fallback, 1) do
    case lookup(name) do
      {:ok, pid} -> answered(pid, message, timeout, fallback)
      :error -> fallback.(:down)
    end
  end

  defp answered(pid, message, timeout, fallback) do
    case safe_call(pid, message, timeout) do
      {:ok, reply} -> reply
      :error -> fallback.({:unresponsive, pid})
    end
  end
end
