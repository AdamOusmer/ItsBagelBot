# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TestSupport.NatsServer do
  @moduledoc "Spawns a real local nats-server owned by the calling process."

  @supervisor ~S("$0" "$@" & server=$!; trap 'kill $server 2>/dev/null; exit' TERM INT; read -r line; kill $server 2>/dev/null; wait $server)

  def start do
    exe = System.find_executable("nats-server") || raise "nats-server not found on PATH"
    port = free_port()

    Port.open({:spawn_executable, "/bin/sh"}, [
      :binary,
      :exit_status,
      args: ["-c", @supervisor, exe, "-p", Integer.to_string(port), "-a", "127.0.0.1"]
    ])

    await_ready(port, 50)
    port
  end

  defp free_port do
    {:ok, socket} = :gen_tcp.listen(0, [])
    {:ok, port} = :inet.port(socket)
    :gen_tcp.close(socket)
    port
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
