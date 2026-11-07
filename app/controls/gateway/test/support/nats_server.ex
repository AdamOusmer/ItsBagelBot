# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.NatsServer do
  @moduledoc """
  Spawns a real local nats-server for integration tests (no JetStream, no
  auth: just core pub/sub, which is all this service uses).
  """

  @spec free_port() :: :inet.port_number()
  def free_port do
    {:ok, socket} = :gen_tcp.listen(0, [])
    {:ok, port} = :inet.port(socket)
    :gen_tcp.close(socket)
    port
  end

  @spec start(:inet.port_number()) :: non_neg_integer()
  def start(port) do
    exe = System.find_executable("nats-server") || raise "nats-server not found on PATH"

    server =
      Port.open({:spawn_executable, exe}, [
        :binary,
        :exit_status,
        args: ["-p", Integer.to_string(port), "-a", "127.0.0.1"]
      ])

    {:os_pid, os_pid} = :erlang.port_info(server, :os_pid)
    await_ready(port, 50)
    os_pid
  end

  # By OS pid: on_exit runs after the test process, and with it the port, is gone.
  @spec stop(non_neg_integer()) :: :ok
  def stop(os_pid) do
    System.cmd("kill", [Integer.to_string(os_pid)])
    :ok
  end

  defp await_ready(_port, 0), do: raise("nats-server did not become ready")

  defp await_ready(port, attempts) do
    case :gen_tcp.connect({127, 0, 0, 1}, port, [:binary, active: false], 200) do
      {:ok, socket} ->
        :gen_tcp.close(socket)

      {:error, _reason} ->
        Process.sleep(100)
        await_ready(port, attempts - 1)
    end
  end
end
