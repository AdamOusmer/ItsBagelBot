# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.EnvCase do
  @moduledoc "Application-env overrides and condition polling for `async: false` suites."

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
