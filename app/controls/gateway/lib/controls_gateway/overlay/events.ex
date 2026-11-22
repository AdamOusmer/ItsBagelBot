# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Events do
  require Logger

  alias ControlsGateway.Overlay.Nats
  alias ControlsGateway.SignedCookie

  @ping_ms 15_000
  @check_ms 300_000
  @retry_ms 3_000..8_000
  @unavailable_retry_ms 15_000..30_000
  @ping_frame ": ping\n\n"

  def open(req, session, headers) do
    case Nats.check(session) do
      {:ok, overlay} ->
        start(req, headers, session, overlay)

      {:denied, reason} ->
        Logger.info("Overlay events link no longer valid",
          overlay_id: session.overlay_id,
          reason: reason
        )

        {:ok, :cowboy_req.reply(204, headers, req), nil}

      :unavailable ->
        Logger.warning("Overlay events unavailable", overlay_id: session.overlay_id)
        body = initial_frame(@unavailable_retry_ms)
        {:ok, :cowboy_req.reply(200, event_headers(headers), body, req), nil}
    end
  end

  defp start(req, headers, session, overlay) do
    req = :cowboy_req.stream_reply(200, event_headers(headers), req)
    :ok = :cowboy_req.stream_body(initial_frame(@retry_ms), :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    schedule_check(session)
    {:cowboy_loop, req, %{session: session, revision: overlay.revision}, :hibernate}
  end

  def info(:ping, req, state) do
    :ok = :cowboy_req.stream_body(@ping_frame, :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    continue(req, state)
  end

  def info(:check, req, state) do
    with false <- state.session.exp <= SignedCookie.now(),
         {:ok, overlay} <- Nats.check(state.session) do
      schedule_check(state.session)
      announce(req, state, overlay.revision)
    else
      true -> stop(req, state, :session_expired)
      {:denied, reason} -> stop(req, state, reason)
      :unavailable -> stop(req, state, :check_failed)
    end
  end

  def info(:shutdown_close, req, state), do: stop(req, state, :shutdown)

  def info(_message, req, state), do: continue(req, state)

  defp announce(req, %{revision: revision} = state, revision), do: continue(req, state)

  defp announce(req, state, revision) do
    frame = ["event: revision\ndata: ", Integer.to_string(revision), "\n\n"]
    :ok = :cowboy_req.stream_body(frame, :nofin, req)
    continue(req, %{state | revision: revision})
  end

  defp schedule_check(session) do
    interval = Application.get_env(:controls_gateway, :overlay_check_ms, @check_ms)
    delay = min(interval, max(0, session.exp - SignedCookie.now()) * 1_000)
    Process.send_after(self(), :check, delay)
  end

  defp initial_frame(retry_ms), do: "retry: #{Enum.random(retry_ms)}\n" <> @ping_frame
  defp event_headers(headers), do: Map.put(headers, "content-type", "text/event-stream")
  defp continue(req, state), do: {:ok, req, state, :hibernate}

  defp stop(req, state, reason) do
    Logger.info("Overlay events stream closed",
      overlay_id: state.session.overlay_id,
      reason: reason
    )

    :cowboy_req.stream_body("", :fin, req)
    {:stop, req, state}
  end
end
