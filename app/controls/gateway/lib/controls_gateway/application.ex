# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Application do
  use Application

  alias ControlsGateway.{Config, Drain}

  @impl true
  def start(_type, _args) do
    children = base_children() ++ server_children()
    Supervisor.start_link(children, strategy: :one_for_one, name: ControlsGateway.Supervisor)
  end

  @impl true
  def prep_stop(state) do
    if Config.server?(), do: Drain.run()
    state
  end

  defp base_children do
    [
      ControlsGateway.Metrics,
      {Registry, keys: :unique, name: ControlsGateway.Connections}
    ]
  end

  defp server_children do
    if Config.server?() do
      [connection_child(:gnat_connection, :gnat, Config.nats()), ControlsGateway.HttpServer]
    else
      []
    end
  end

  defp connection_child(id, name, connection_settings) do
    Supervisor.child_spec(
      {Gnat.ConnectionSupervisor,
       %{name: name, backoff_period: 4_000, connection_settings: connection_settings}},
      id: id
    )
  end
end
