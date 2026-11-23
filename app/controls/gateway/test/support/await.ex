# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.Await do
  @moduledoc "Polling and counter helpers for the integration tests."

  import ExUnit.Assertions, only: [flunk: 1]

  def eventually(fun, attempts \\ 40) do
    cond do
      fun.() ->
        :ok

      attempts == 0 ->
        flunk("condition not met")

      true ->
        Process.sleep(100)
        eventually(fun, attempts - 1)
    end
  end

  def put_env(key, value) do
    previous = Application.fetch_env(:controls_gateway, key)
    Application.put_env(:controls_gateway, key, value)

    ExUnit.Callbacks.on_exit(fn ->
      case previous do
        {:ok, old} -> Application.put_env(:controls_gateway, key, old)
        :error -> Application.delete_env(:controls_gateway, key)
      end
    end)
  end

  def counter(name), do: Platform.Metrics.snapshot(ControlsGateway.Metrics)[name] || 0
end
