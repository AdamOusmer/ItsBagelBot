# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Events do
  require Logger

  alias ControlsGateway.Overlay.{Feed, Nats}
  alias ControlsGateway.{SignedCookie, Subjects}
  alias Platform.Nats.{Planes, Push}

  @ping_ms 15_000
  @retry_ms 3_000..8_000
  @unavailable_retry_ms 15_000..30_000
  @ping_frame ": ping\n\n"

  def open(req, session, headers) do
    with %Planes{} = jetstream <- Planes.connection(:jetstream),
         %Planes{} = rpc <- Planes.connection(:rpc),
         {:ok, link} <- subscribe(rpc, session),
         {:ok, overlay} <- Nats.check(session),
         {:ok, chat_sid} <- Planes.subscribe(rpc, self(), Subjects.overlay_chat(overlay.user_id)),
         last_event_id = :cowboy_req.header("last-event-id", req, nil),
         {:ok, start, snapshot} <- Feed.start(jetstream, rpc, overlay.user_id, last_event_id),
         {:ok, push} <-
           Push.open(
             jetstream,
             Feed.stream(),
             Subjects.overlay_feed(overlay.user_id),
             start,
             "feed"
           ) do
      state = %{
        session: session,
        user_id: overlay.user_id,
        revision: overlay.revision,
        jetstream: jetstream,
        rpc: rpc,
        monitor: Process.monitor(jetstream.pid),
        rpc_monitor: Process.monitor(rpc.pid),
        revised_sid: link.revised,
        revoked_sid: link.revoked,
        chat_sid: chat_sid,
        push: push
      }

      start(req, headers, state, snapshot)
    else
      {:denied, reason} ->
        Logger.info("Overlay events link no longer valid",
          overlay_id: session.overlay_id,
          reason: reason
        )

        {:ok, :cowboy_req.reply(204, headers, req), nil}

      other ->
        Logger.warning("Overlay events unavailable",
          overlay_id: session.overlay_id,
          reason: inspect(other, limit: 5)
        )

        body = initial_frame(@unavailable_retry_ms)
        {:ok, :cowboy_req.reply(200, event_headers(headers), body, req), nil}
    end
  end

  defp subscribe(rpc, session) do
    with {:ok, revised} <-
           Planes.subscribe(rpc, self(), Subjects.overlay_revised(session.overlay_id)),
         {:ok, revoked} <-
           Planes.subscribe(rpc, self(), Subjects.overlay_revoked(session.overlay_id)) do
      {:ok, %{revised: revised, revoked: revoked}}
    end
  end

  defp start(req, headers, state, snapshot) do
    state = attach(state)
    req = :cowboy_req.stream_reply(200, event_headers(headers), req)
    :ok = :cowboy_req.stream_body([initial_frame(@retry_ms), snapshot], :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    schedule_expiry(state.session)
    {:cowboy_loop, req, state, :hibernate}
  end

  defp attach(state) do
    case Push.attach(state.push) do
      {:ok, push} ->
        %{state | push: push}

      {:error, reason} ->
        log_attach_failure(state, reason)
        state
    end
  end

  def info({:msg, %{sid: sid, gnat: rpc} = message}, req, %{rpc: %{pid: rpc}} = state) do
    cond do
      sid == state.revoked_sid -> stop(req, state, :revoked)
      sid == state.revised_sid -> announce(req, state, message.body)
      sid == state.chat_sid -> chat(req, state, message.body)
      true -> continue(req, state)
    end
  end

  def info({:msg, message}, req, state) do
    if Push.owns?(state.push, message),
      do: deliver(req, state, message),
      else: continue(req, state)
  end

  def info({:DOWN, ref, :process, _pid, _reason}, req, state)
      when ref == state.monitor or ref == state.rpc_monitor,
      do: stop(req, state, down_reason(ref, state))

  def info(:ping, req, state) do
    case Push.tick(state.push) do
      :stale ->
        stop(req, state, :stale)

      result ->
        :ok = :cowboy_req.stream_body(@ping_frame, :nofin, req)
        Process.send_after(self(), :ping, @ping_ms)
        continue(req, ticked(state, result))
    end
  end

  def info(:expired, req, state), do: stop(req, state, :session_expired)
  def info(:shutdown_close, req, state), do: stop(req, state, :shutdown)
  def info(_message, req, state), do: continue(req, state)

  defp ticked(state, {:ok, push}), do: %{state | push: push}

  defp ticked(state, {:error, reason}) do
    log_attach_failure(state, reason)
    state
  end

  defp deliver(req, state, message) do
    case Push.handle(state.push, message) do
      {:ok, push} ->
        continue(req, %{state | push: push})

      {:event, %{seq: seq, topic: topic, body: body}, push} ->
        send_frame(req, Feed.frame(topic, seq, body), state)
        continue(req, %{state | push: push})

      {:error, reason} ->
        stop(req, state, reason)
    end
  end

  defp chat(req, state, body) do
    case Feed.chat_frame(body) do
      {:ok, frame} -> :cowboy_req.stream_body(frame, :nofin, req)
      :error -> Logger.warning("Overlay chat line invalid", overlay_id: state.session.overlay_id)
    end

    continue(req, state)
  end

  defp send_frame(_req, :error, state),
    do: Logger.warning("Overlay feed frame invalid", overlay_id: state.session.overlay_id)

  defp send_frame(req, frame, _state), do: :cowboy_req.stream_body(frame, :nofin, req)

  defp announce(req, state, body) do
    case Integer.parse(body) do
      {revision, ""} when revision != state.revision ->
        :ok = :cowboy_req.stream_body(["event: revision\ndata: ", body, "\n\n"], :nofin, req)
        continue(req, %{state | revision: revision})

      _ ->
        continue(req, state)
    end
  end

  defp log_attach_failure(state, reason) do
    Logger.warning("Overlay feed consumer unavailable",
      overlay_id: state.session.overlay_id,
      reason: inspect(reason, limit: 5)
    )
  end

  defp down_reason(ref, %{monitor: ref}), do: :nats_down
  defp down_reason(_ref, _state), do: :rpc_down

  defp schedule_expiry(session) do
    Process.send_after(self(), :expired, max(0, session.exp - SignedCookie.now()) * 1_000)
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
