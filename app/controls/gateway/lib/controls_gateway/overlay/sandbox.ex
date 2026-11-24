# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Sandbox do
  alias ControlsGateway.{Config, Http}

  @path Path.expand("../../../priv/overlay/frame.js", __DIR__)
  @external_resource @path
  @frame File.read!(@path)

  if String.contains?(@frame, "</script") do
    raise "priv/overlay/frame.js must not contain a closing script tag"
  end

  @head "<!doctype html><meta charset=\"utf-8\"><meta name=\"referrer\" content=\"no-referrer\">" <>
          "<meta name=\"bagel-parent\" content=\""
  @tail "\"><style>html,body{margin:0;background:transparent;overflow:hidden}</style>" <>
          "<body><script>" <> @frame <> "</script>"

  @cdn "https://cdn.itsbagelbot.com"
  @policy "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; " <>
            "style-src 'unsafe-inline'; img-src #{@cdn}; media-src #{@cdn}; font-src #{@cdn}; " <>
            "connect-src 'none'; form-action 'none'; base-uri 'none'"

  @headers %{
    "content-type" => "text/html; charset=utf-8",
    "cache-control" => "public, max-age=3600",
    "content-security-policy" => @policy,
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  def reply(conn, widget) do
    if String.match?(widget, ~r/\A[a-zA-Z0-9_-]{1,96}\z/),
      do: Http.reply(conn, 200, @headers, [@head, Config.overlay_origin(), @tail]),
      else: Http.reply(conn, 404, %{"cache-control" => "no-store"})
  end
end
