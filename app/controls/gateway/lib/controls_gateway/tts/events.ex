# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Events do
  require Logger

  alias ControlsGateway.Overlays.{Handle, Pending}
  alias ControlsGateway.Subjects
  alias ControlsGateway.Tts.{Nats, Session}
  alias Platform.Nats.Planes

  @ping_ms 15_000
  @check_ms 300_000
  @recheck_ms 15_000
  @retry_ms 3_000..8_000
  @unavailable_retry_ms 15_000..30_000
  @ping_frame ": ping\n\n"
  @accepted ~s({"accepted":true})

  def open(req, session, headers) do
    with %Planes{} = jetstream <- Planes.connection(:jetstream),
         %Planes{} = rpc <- Planes.connection(:rpc),
         {:ok, revoked_sid} <-
           Planes.subscribe(rpc, self(), "controls.tts.revoked.#{session.user_id}"),
         true <- Nats.check(session, rpc),
         {:ok, clip_sid} <-
           Planes.subscribe(jetstream, self(), Subjects.overlay_session(session.user_id)) do
      state = %{
        jetstream: jetstream,
        rpc: rpc,
        monitor: Process.monitor(jetstream.pid),
        rpc_monitor: Process.monitor(rpc.pid),
        session: session,
        revoked_sid: revoked_sid,
        clip_sid: clip_sid,
        pending: Pending.start(self())
      }

      start(req, headers, state)
    else
      false ->
        Logger.warning("TTS events link no longer valid", user_id: session.user_id)
        {:ok, :cowboy_req.reply(204, headers, req), nil}

      other ->
        Logger.warning("TTS events unavailable",
          user_id: session.user_id,
          reason: inspect(other, limit: 5)
        )

        body = initial_frame(@unavailable_retry_ms)
        {:ok, :cowboy_req.reply(200, event_headers(headers), body, req), nil}
    end
  end

  defp start(req, headers, state) do
    Logger.info("TTS events stream opened", user_id: state.session.user_id)

    req = :cowboy_req.stream_reply(200, event_headers(headers), req)
    :ok = :cowboy_req.stream_body(initial_frame(@retry_ms), :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    schedule_check(state.session, @check_ms)
    {:cowboy_loop, req, state, :hibernate}
  end

  defp initial_frame(retry_ms), do: "retry: #{Enum.random(retry_ms)}\n" <> @ping_frame

  def info({:msg, %{sid: sid, gnat: rpc}}, req, %{revoked_sid: sid, rpc: %{pid: rpc}} = state),
    do: stop(req, state, :revoked)

  def info({:msg, %{sid: sid, gnat: pid} = message}, req, state)
      when sid == state.clip_sid and pid == state.jetstream.pid,
      do: deliver(message, req, state)

  def info({:DOWN, ref, :process, _pid, _reason}, req, state)
      when ref == state.monitor or ref == state.rpc_monitor,
      do: stop(req, state, down_reason(ref, state))

  def info(:ping, req, state) do
    :ok = :cowboy_req.stream_body(@ping_frame, :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    continue(req, state)
  end

  def info(:check, req, state) do
    case verdict(state) do
      :valid ->
        schedule_check(state.session, @check_ms)
        continue(req, state)

      :unavailable ->
        schedule_check(state.session, @recheck_ms)
        continue(req, state)

      {:invalid, reason} ->
        stop(req, state, reason)
    end
  end

  def info(:shutdown_close, req, state), do: stop(req, state, :shutdown)

  def info(_message, req, state), do: continue(req, state)

  defp deliver(%{reply_to: reply, body: body}, req, state) when is_binary(reply) do
    case Jason.decode(body) do
      {:ok, %{"ack" => ack, "kind" => kind, "data" => %{} = data}} when is_binary(ack) ->
        send_clip(req, {ack, kind}, data, state)
        Planes.publish(state.jetstream, reply, @accepted)

      _ ->
        Logger.warning("TTS clip offer invalid", user_id: state.session.user_id)
    end

    continue(req, state)
  end

  defp deliver(_message, req, state), do: continue(req, state)

  defp send_clip(req, {ack, kind}, event, %{session: session, pending: pending}) do
    Pending.track(pending, ack)
    clip = Map.put(event, "handle", Handle.sign(session.user_id, ack))
    frame = ["event: ", kind, "\ndata: ", Jason.encode!(clip), "\n\n"]
    :cowboy_req.stream_body(frame, :nofin, req)

    Logger.info("TTS event delivered",
      user_id: session.user_id,
      event_id: event["event_id"],
      trace_id: event["trace_id"]
    )
  end

  defp verdict(state) do
    if state.session.exp <= Session.now() do
      {:invalid, :session_expired}
    else
      check_verdict(Nats.check(state.session, state.rpc))
    end
  end

  defp check_verdict(true), do: :valid
  defp check_verdict(false), do: {:invalid, :check_failed}
  defp check_verdict(:unavailable), do: :unavailable

  defp down_reason(ref, %{monitor: ref}), do: :nats_down
  defp down_reason(_ref, _state), do: :rpc_down

  defp schedule_check(session, interval_ms) do
    delay = min(interval_ms, max(0, session.exp - Session.now()) * 1_000)
    Process.send_after(self(), :check, delay)
  end

  defp event_headers(headers), do: Map.put(headers, "content-type", "text/event-stream")
  defp continue(req, state), do: {:ok, req, state, :hibernate}

  defp stop(req, state, reason) do
    log_stop(reason, state.session.user_id)
    :cowboy_req.stream_body("", :fin, req)
    {:stop, req, state}
  end

  defp log_stop(reason, user_id) when reason in [:shutdown, :revoked],
    do: Logger.info("TTS events stream closed", user_id: user_id, reason: reason)

  defp log_stop(reason, user_id),
    do: Logger.warning("TTS events stream closed", user_id: user_id, reason: reason)
end
