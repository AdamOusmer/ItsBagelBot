# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.EnvCase do
  @moduledoc """
  Application-env overrides and condition polling for the `async: false` suites.

  Plain module, imported rather than `use`d: the suites that need it already
  pick their own case template (`Ingress.PublisherCase`, or bare
  `ExUnit.Case`), and a second template would force them to choose.

  It lives here because every copy of the save/restore pair drifted in the same
  direction: hand-listing the keys to restore, so a key added to the override
  list leaks into the next test, and one suite restored a *guessed* value
  (`MapSet.new()`) instead of the one it displaced. `put_env/1` derives the
  restore list from the overrides themselves, so neither is possible.
  """

  @doc """
  Applies `:ingress` application-env overrides for the duration of the test and
  restores the previous values (deleting keys that were unset) afterwards.

  The `on_exit` is registered here, so teardown unwinds in reverse against
  anything the caller registers afterwards — the hot-path snapshot is
  uninstalled before the config it was installed from is restored.
  """
  @spec put_env(keyword()) :: :ok
  def put_env(overrides) do
    previous = Enum.map(overrides, fn {key, _} -> {key, Application.get_env(:ingress, key)} end)
    Enum.each(overrides, fn {key, value} -> Application.put_env(:ingress, key, value) end)
    ExUnit.Callbacks.on_exit(fn -> Enum.each(previous, &restore_env/1) end)
    :ok
  end

  @spec restore_env(atom(), term()) :: :ok
  def restore_env(key, nil), do: Application.delete_env(:ingress, key)
  def restore_env(key, value), do: Application.put_env(:ingress, key, value)

  defp restore_env({key, value}), do: restore_env(key, value)

  @doc """
  Polls `check` on a 10ms tick until it holds, up to `attempts` times.

  Returns a boolean rather than flunking: the caller wraps it in `assert`, so
  the failure is reported against the calling line and not against a helper
  three files away.
  """
  @spec eventually((-> boolean()), non_neg_integer()) :: boolean()
  def eventually(check, attempts \\ 100)

  def eventually(check, attempts) do
    cond do
      check.() ->
        true

      attempts == 0 ->
        false

      true ->
        Process.sleep(10)
        eventually(check, attempts - 1)
    end
  end
end
