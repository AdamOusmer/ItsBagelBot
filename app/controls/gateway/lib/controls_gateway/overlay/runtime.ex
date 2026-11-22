# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Runtime do
  @path Path.expand("../../../priv/overlay/runtime.js", __DIR__)
  @external_resource @path
  @js File.read!(@path)
  @name Base.encode16(:binary.part(:crypto.hash(:sha256, @js), 0, 16), case: :lower) <> ".js"

  @headers %{
    "content-type" => "text/javascript; charset=utf-8",
    "cache-control" => "public, max-age=31536000, immutable",
    "x-content-type-options" => "nosniff"
  }

  def path, do: "/o/runtime/" <> @name

  def reply(req, @name), do: :cowboy_req.reply(200, @headers, @js, req)
  def reply(req, _name), do: :cowboy_req.reply(404, %{"cache-control" => "no-store"}, req)
end
