# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Application do
  use Application

  alias ControlsGateway.{Caches, Config, Drain, HttpServer, Overlays}
  alias Platform.Nats.Planes

  @impl true
  def start(_type, _args) do
    ControlsGateway.Metrics.attach_pull()
    ControlsGateway.Metrics.attach_cache()

    Platform.Startup.start_link(
      name: ControlsGateway.Supervisor,
      enabled: Config.server?(),
      base: &base_children/0,
      service: &server_children/0
    )
  end

  @impl true
  def prep_stop(state), do: Platform.Startup.prep_stop(state, Config.server?(), &Drain.run/0)

  defp base_children do
    [
      ControlsGateway.Metrics,
      {Registry, keys: :unique, name: ControlsGateway.Connections},
      {Registry, keys: :duplicate, name: ControlsGateway.Streams}
    ]
  end

  defp server_children do
    Planes.children(Config.nats()) ++ Overlays.children() ++ [Caches, HttpServer]
  end
end
