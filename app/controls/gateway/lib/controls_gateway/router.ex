# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Router do
  use Plug.Router

  alias ControlsGateway.{Drain, Http, Overlay, Overlays, Powered, Tts}

  plug(:match)
  plug(:dispatch)

  match("/healthz", do: Http.reply(conn, 200, %{}, "ok\n"))
  match("/readyz", do: readyz(conn))

  match("/tts", do: Tts.Http.handle(conn, :page))
  match("/tts/session", do: Tts.Http.handle(conn, :session))
  match("/tts/ack", do: Tts.Http.handle(conn, :ack))
  match("/tts/progress", do: Tts.Http.handle(conn, :progress))
  match("/tts/clip/:event_id", do: Tts.Http.handle(conn, :clip))

  match("/o", do: Overlay.Http.handle(conn, :page))
  match("/o/session", do: Overlay.Http.handle(conn, :session))
  match("/o/doc", do: Overlay.Http.handle(conn, :doc))
  match("/o/ack", do: Overlay.Http.handle(conn, :ack))
  match("/o/progress", do: Overlay.Http.handle(conn, :progress))
  match("/powered/config", do: Overlay.Http.handle(conn, :config))

  match("/powered", do: Powered.Http.handle(conn, :page))
  match("/powered/assets/:name", do: Powered.Http.handle(conn, :asset))

  match(_, do: Http.reply(conn, 404))

  defp readyz(conn) do
    report =
      Platform.Health.report("controls-gateway", [
        {"draining", &Drain.ready?/0},
        {"jetstream", :gnat_jetstream},
        {"rpc", :gnat_rpc},
        {"pullers", &Overlays.pullers_attached?/0}
      ])

    if Platform.Health.up?(report),
      do: Http.reply(conn, 200, %{}, "ok\n"),
      else: Http.reply(conn, 503, %{}, "not ready\n")
  end
end
