# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.Peer do
  @moduledoc "Starts a second gateway in a separate BEAM, on its own listener, against the test NATS servers."

  alias Platform.Nats.Planes
  alias Platform.Nats.TestSupport.NatsServer

  @call_timeout_ms 30_000

  @spec start_gateway(map()) :: :inet.port_number()
  def start_gateway(%{rpc_port: rpc_port, jetstream_port: jetstream_port}) do
    port = NatsServer.free_port()
    endpoints = %{rpc: {"127.0.0.1", rpc_port}, jetstream: {"127.0.0.1", jetstream_port}}

    gateway =
      :controls_gateway
      |> Application.get_all_env()
      |> Keyword.merge(server: true, port: port, nats: Planes.settings(endpoints, %{}, nil))

    env =
      for {app, _description, _version} <- Application.loaded_applications() do
        {app, if(app == :controls_gateway, do: gateway, else: Application.get_all_env(app))}
      end

    peer =
      ExUnit.Callbacks.start_supervised!(%{
        id: __MODULE__,
        start: {:peer, :start_link, [%{connection: :standard_io}]}
      })

    call(peer, :code, :add_pathsa, [:code.get_path()])
    :ok = call(peer, :application, :load, [:controls_gateway])
    :ok = call(peer, :application, :set_env, [env, [persistent: true]])
    {:ok, _} = call(peer, :application, :ensure_all_started, [:elixir])
    {:ok, _} = call(peer, :application, :ensure_all_started, [:controls_gateway])
    port
  end

  defp call(peer, module, function, args),
    do: :peer.call(peer, module, function, args, @call_timeout_ms)
end
