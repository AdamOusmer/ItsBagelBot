# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.HttpServer do
  @moduledoc "Owns the Ranch listener for the TTS beta and health routes."
  use GenServer

  alias ControlsGateway.{Config, HttpHandler, Tts}

  @listener :controls_gateway_http

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec listener() :: atom()
  def listener, do: @listener

  @impl true
  def init(opts) do
    # Without this, a supervisor shutdown signal kills the process directly
    # and skips terminate/2, leaking the Ranch listener under its fixed name.
    Process.flag(:trap_exit, true)
    port = Keyword.get(opts, :port, Config.port())

    routes = Keyword.get(opts, :routes, tts_routes())

    health = [
      {"/healthz", HttpHandler, :healthz},
      {"/readyz", HttpHandler, :readyz}
    ]

    dispatch = :cowboy_router.compile([{:_, health ++ routes}])
    protocol_opts = %{env: %{dispatch: dispatch}, reset_idle_timeout_on_send: true}
    tls = Application.get_env(:controls_gateway, :listener_tls, [])
    transport_opts = %{socket_opts: [port: port] ++ tls, max_connections: :infinity}

    {:ok, _pid} = start_listener(tls, transport_opts, protocol_opts)

    {:ok, %{}}
  end

  defp tts_routes do
    [
      {"/tts", Tts.Http, :page},
      {"/tts/session", Tts.Http, :session},
      {"/tts/events", Tts.Http, :events},
      {"/tts/clip/:event_id", Tts.Http, :clip}
    ]
  end

  defp start_listener([], transport, protocol),
    do: :cowboy.start_clear(@listener, transport, protocol)

  defp start_listener(_tls, transport, protocol),
    do: :cowboy.start_tls(@listener, transport, protocol)

  @impl true
  def terminate(_reason, _state) do
    :cowboy.stop_listener(@listener)
    :ok
  end
end
