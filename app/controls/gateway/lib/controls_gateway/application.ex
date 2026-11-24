# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Application do
  use Application

  alias ControlsGateway.{Caches, Config, Drain, HttpServer, Overlays}
  alias Platform.Nats.{HubFailback, LeafFailback, Planes}

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
    nats = Config.nats()
    hub = Planes.connection_name(:jetstream)

    Planes.children(nats) ++
      [
        {LeafFailback, connections: [Planes.connection_name(:rpc)]},
        {HubFailback,
         connections: fn -> [{hub, fn -> Platform.Nats.stop(hub) end}] end,
         probe_settings: HubFailback.local_probe_settings(hd(nats.jetstream))}
      ] ++
      Overlays.children() ++ [Caches, HttpServer]
  end
end
