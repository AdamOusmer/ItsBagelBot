# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Singleton do
  @registry Ingress.Registry

  @type name :: term()
  @type reason :: :down | {:unresponsive, pid()}

  @spec lookup(name()) :: {:ok, pid()} | :error
  def lookup(name) do
    case Horde.Registry.lookup(@registry, name) do
      [{pid, _value}] -> {:ok, pid}
      _unheld -> :error
    end
  end

  @spec safe_call(pid(), term(), timeout()) :: {:ok, term()} | :error
  def safe_call(pid, message, timeout) do
    {:ok, GenServer.call(pid, message, timeout)}
  catch
    :exit, _reason -> :error
  end

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
