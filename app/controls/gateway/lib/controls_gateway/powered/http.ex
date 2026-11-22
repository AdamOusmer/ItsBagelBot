# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Http do
  @behaviour :cowboy_handler

  alias ControlsGateway.Powered.{Assets, Page}

  @headers %{
    "cache-control" => "no-store",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff",
    "allow" => "GET"
  }

  @impl true
  def init(req, route), do: {:ok, dispatch(:cowboy_req.method(req), route, req), route}

  defp dispatch("GET", :page, req), do: Page.reply(req)
  defp dispatch("GET", :asset, req), do: Assets.reply(req, :cowboy_req.binding(:name, req))
  defp dispatch(_method, _route, req), do: :cowboy_req.reply(405, @headers, req)
end
