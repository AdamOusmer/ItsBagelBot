# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Http do
  alias ControlsGateway.Http
  alias ControlsGateway.Powered.{Assets, Page}

  @headers %{
    "cache-control" => "no-store",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff",
    "allow" => "GET"
  }

  def handle(%{method: "GET"} = conn, :page), do: Page.reply(conn)
  def handle(%{method: "GET"} = conn, :asset), do: Assets.reply(conn, conn.path_params["name"])
  def handle(conn, _route), do: Http.reply(conn, 405, @headers)
end
