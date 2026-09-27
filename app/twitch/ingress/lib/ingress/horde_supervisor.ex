# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HordeSupervisor do
  @moduledoc false

  @request_timeout_ms 2_000
  @termination_timeout_ms 6_000

  # Horde 0.10 uses these GenServer messages internally, but its public wrappers
  # always wait forever. Keep the pinned protocol here so dropped forwarded
  # replies cannot stop ingress reconciliation or its bootstrapper.
  def start_child(supervisor, child_spec, timeout \\ @request_timeout_ms) do
    spec = Supervisor.child_spec(child_spec, [])
    request(supervisor, {:start_child, spec}, timeout)
  end

  def terminate_child(supervisor, pid, timeout \\ @termination_timeout_ms) do
    request(supervisor, {:terminate_child, pid}, timeout)
  end

  def which_children(supervisor, timeout \\ @request_timeout_ms) do
    request(supervisor, :which_children, timeout)
  end

  defp request(supervisor, message, timeout) do
    supervisor
    |> GenServer.call(message, timeout)
    |> normalize_reply()
  catch
    :exit, {:timeout, _call} -> {:error, :timeout}
    :exit, reason -> {:error, {:supervisor_unavailable, reason}}
  end

  defp normalize_reply({:error, :proxy_operation_ttl_expired, message}),
    do: {:error, {:proxy_operation_ttl_expired, message}}

  defp normalize_reply(reply), do: reply
end
