# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.HttpServer do
  @moduledoc "Listener child spec: the Plug router plus the streaming and desktop loop routes."

  alias ControlsGateway.{Config, EventsHandler, Router}
  alias Platform.Cowboy.Server

  @listener :controls_gateway_http

  @spec listener() :: atom()
  def listener, do: @listener

  @spec child_spec(keyword()) :: Supervisor.child_spec()
  def child_spec(opts) do
    tls = Application.get_env(:controls_gateway, :listener_tls, [])
    extra_routes = Keyword.get(opts, :routes, [])

    Server.child_spec(
      [
        name: @listener,
        plug: Router,
        port: Keyword.get(opts, :port, Config.port()),
        num_acceptors: 10,
        max_connections: :infinity,
        reset_idle_timeout_on_send: true,
        protocols: [:http2, :http],
        loop_routes: stream_routes() ++ extra_routes
      ] ++ scheme(tls)
    )
  end

  defp stream_routes do
    [
      {"/tts/events", EventsHandler, :tts},
      {"/o/events", EventsHandler, :overlay}
    ]
  end

  defp scheme([]), do: [scheme: :http]
  defp scheme(tls), do: [scheme: :https] ++ Keyword.take(tls, [:certfile, :keyfile])
end
