# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Application do
  use Application

  alias ControlsGateway.{Config, Drain}
  alias ControlsGateway.Overlays.{Puller, Tiers}
  alias Platform.Nats.Planes

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
      ControlsGateway.Tts.Throttle,
      {Registry, keys: :unique, name: ControlsGateway.Connections}
    ]
  end

  defp pullers, do: Enum.map(Tiers.all(), &{Puller, &1})

  defp server_children do
    if Config.server?() do
      Planes.children(Config.nats()) ++ pullers() ++ [ControlsGateway.HttpServer]
    else
      []
    end
  end
end
