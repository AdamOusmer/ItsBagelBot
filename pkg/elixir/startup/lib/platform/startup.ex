# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Startup do
  @moduledoc "Builds a service supervisor with explicit enabled lifecycle hooks."

  @noop &__MODULE__.noop/0

  def start_link(opts) do
    {install, uninstall, service} = hooks(opts, Keyword.fetch!(opts, :enabled))
    base = Keyword.fetch!(opts, :base)
    name = Keyword.fetch!(opts, :name)

    fn ->
      install.()
      Supervisor.start_link(base.() ++ service.(), strategy: :one_for_one, name: name)
    end
    |> guarded(uninstall)
    |> undo_on_error(uninstall)
  end

  def prep_stop(state, enabled, hook) do
    if enabled, do: hook.()
    state
  end

  def stop(hook) do
    hook.()
    :ok
  end

  @doc false
  def noop, do: :ok

  defp hooks(opts, true) do
    {Keyword.get(opts, :install, @noop), Keyword.get(opts, :uninstall, @noop),
     Keyword.fetch!(opts, :service)}
  end

  defp hooks(opts, false) do
    Keyword.fetch!(opts, :service)
    {@noop, @noop, fn -> [] end}
  end

  defp guarded(start, uninstall) do
    start.()
  catch
    kind, reason ->
      uninstall.()
      :erlang.raise(kind, reason, __STACKTRACE__)
  end

  defp undo_on_error({:ok, _} = ok, _uninstall), do: ok

  defp undo_on_error(error, uninstall) do
    uninstall.()
    error
  end
end
