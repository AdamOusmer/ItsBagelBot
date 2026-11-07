# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.HttpServer do
  @moduledoc "Owns the Ranch listener for /v1/connect and the health routes."
  use GenServer

  alias ControlsGateway.{Config, HttpHandler, WsHandler}

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

    dispatch =
      :cowboy_router.compile([
        {:_,
         [
           {"/v1/connect", WsHandler, []},
           {"/healthz", HttpHandler, :healthz},
           {"/readyz", HttpHandler, :readyz},
           {"/metrics", HttpHandler, :metrics}
         ]}
      ])

    {:ok, _pid} =
      :cowboy.start_clear(@listener, %{socket_opts: [port: port]}, %{env: %{dispatch: dispatch}})

    {:ok, %{}}
  end

  @impl true
  def terminate(_reason, _state) do
    :cowboy.stop_listener(@listener)
    :ok
  end
end
