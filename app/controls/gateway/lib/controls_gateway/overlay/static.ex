# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Static do
  @behaviour :cowboy_handler

  alias ControlsGateway.Overlay.{Runtime, Sandbox}

  @headers %{
    "cache-control" => "no-store",
    "x-content-type-options" => "nosniff",
    "allow" => "GET"
  }

  @impl true
  def init(req, route), do: {:ok, dispatch(:cowboy_req.method(req), route, req), route}

  defp dispatch("GET", :runtime, req), do: Runtime.reply(req, :cowboy_req.binding(:name, req))
  defp dispatch("GET", :sandbox, req), do: Sandbox.reply(req, :cowboy_req.binding(:widget, req))
  defp dispatch(_method, _route, req), do: :cowboy_req.reply(405, @headers, req)
end
