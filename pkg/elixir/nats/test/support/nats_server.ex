# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.TestSupport.NatsServer do
  @moduledoc "Spawns real local nats-servers for integration tests."

  alias Platform.Nats
  alias Platform.Nats.Planes

  @supervisor ~S("$0" "$@" & server=$!; trap 'kill $server 2>/dev/null; exit' TERM INT; read -r line; kill $server 2>/dev/null; wait $server)

  @spec free_port() :: :inet.port_number()
  def free_port do
    {:ok, socket} = :gen_tcp.listen(0, [])
    {:ok, port} = :inet.port(socket)
    :gen_tcp.close(socket)
    port
  end

  @doc "Starts a plain server owned by the calling process and returns its port."
  @spec start() :: :inet.port_number()
  def start do
    port = free_port()
    spawn_server(port, nil)
    port
  end

  @doc """
  Starts separate RPC and JetStream servers owned by the calling process and
  supervises their Platform.Nats connections in the test supervisor.
  """
  @spec start_planes(String.t() | nil) :: %{
          rpc: {String.t(), :inet.port_number()},
          jetstream: {String.t(), :inet.port_number()}
        }
  def start_planes(store_dir) do
    endpoints = %{
      rpc: {"127.0.0.1", free_port()},
      jetstream: {"127.0.0.1", free_port()}
    }

    spawn_server(elem(endpoints.rpc, 1), nil)
    spawn_server(elem(endpoints.jetstream, 1), store_dir)

    endpoints
    |> Planes.settings(%{}, nil)
    |> Planes.children(backoff_period: 300)
    |> Enum.each(&ExUnit.Callbacks.start_supervised!/1)

    await_connection(:gnat_rpc)
    await_connection(:gnat_jetstream)
    endpoints
  end

  @spec await_connection(atom(), non_neg_integer()) :: :ok
  def await_connection(name, attempts \\ 50)

  def await_connection(name, 0), do: raise("#{name} did not connect to the test nats-server")

  def await_connection(name, attempts) do
    if Nats.connection_pid(name) do
      :ok
    else
      Process.sleep(50)
      await_connection(name, attempts - 1)
    end
  end

  defp spawn_server(port, store_dir) do
    exe = System.find_executable("nats-server") || raise "nats-server not found on PATH"

    args = ["-p", Integer.to_string(port), "-a", "127.0.0.1"]
    args = if store_dir, do: args ++ ["-js", "-sd", store_dir], else: args

    Port.open({:spawn_executable, "/bin/sh"}, [
      :binary,
      :exit_status,
      args: ["-c", @supervisor, exe | args]
    ])

    await_ready(port, 50)
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
