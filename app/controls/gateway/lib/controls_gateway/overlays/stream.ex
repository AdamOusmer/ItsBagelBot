# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Stream do
  @moduledoc "One SSE stream for the visual overlay and the TTS overlay."

  require Logger

  alias ControlsGateway.{Metrics, Overlay, SignedCookie, Subjects, Tts}
  alias ControlsGateway.Overlay.Feed
  alias ControlsGateway.Overlays.{Delivery, Handle, Pending}
  alias Platform.Nats.Planes

  @ping_ms 15_000
  @check_ms 300_000
  @recheck_ms 15_000
  @retry_ms 1_000..3_000
  @status_retry_after "15"
  @unavailable_retry_ms 15_000..30_000
  @planes_retry_after "1"
  @force_close_ms 10_000
  @down_grace_ms 1_000
  @reconnect_max_ms 1_500
  @ping_frame ": ping\n\n"
  @custom "custom"
  @state_frames ["stats", "credits", "credits_replace", "subathon", "control"]
  @accepted ~s({"accepted":true})
  @labels %{overlay: "Overlay", tts: "TTS"}
  @streams ControlsGateway.Streams

  def open(req, kind, session, headers) do
    base = %{kind: kind, session: session}

    with %Planes{} = jetstream <- Planes.connection(:jetstream),
         %Planes{} = rpc <- Planes.connection(:rpc) do
      admit(req, headers, Map.merge(base, %{jetstream: jetstream, rpc: rpc}))
    else
      nil -> planes_missing(req, headers, base)
    end
  end

  def status(kind, session) do
    with %Planes{} <- Planes.connection(:jetstream),
         %Planes{} <- Planes.connection(:rpc) do
      link_status(kind, session)
    else
      nil -> {503, @planes_retry_after}
    end
  end

  defp link_status(kind, session) do
    case check(kind, session, :rpc) do
      {:ok, _link} ->
        {200, nil}

      {:denied, reason} ->
        Logger.info(
          "#{label(kind)} link no longer valid",
          log_id(kind, session) ++ [reason: reason]
        )

        {204, nil}

      :unavailable ->
        Logger.warning("#{label(kind)} events status unavailable", log_id(kind, session))
        {503, @status_retry_after}
    end
  end

  defp admit(req, headers, state) do
    with {:ok, _} <- register_early(state),
         {:ok, revoked_sid} <- watch_revocation(state),
         {:ok, link} <- check(state.kind, state.session, state.rpc),
         {:ok, offers} <- subscribe_offers(state, link) do
      state = Map.merge(state, Map.merge(link, Map.put(offers, :revoked_sid, revoked_sid)))
      start(req, headers, state)
    else
      {:denied, reason} -> denied(req, headers, state, reason)
      other -> unavailable(req, headers, state, other)
    end
  end

  defp register_early(%{kind: :tts, session: session}),
    do: Registry.register(@streams, session.user_id, nil)

  defp register_early(_state), do: {:ok, nil}

  defp register_late(%{kind: :overlay} = state) do
    {:ok, _} = Registry.register(@streams, state.user_id, nil)
    send(self(), :link_changed)
  end

  defp register_late(_state), do: :ok

  defp subscribe_offers(state, %{user_id: user_id, accepts: accepts}) when is_binary(accepts) do
    group = [queue_group: queue_group(state.kind, accepts)]

    with {:ok, offer_sid} <-
           Planes.subscribe(
             state.jetstream,
             self(),
             Subjects.overlay_session(user_id, accepts),
             group
           ),
         {:ok, state_sid} <- subscribe_state(state, user_id, accepts) do
      {:ok, %{offer_sid: offer_sid, state_sid: state_sid}}
    end
  end

  defp subscribe_offers(_state, _link), do: {:ok, %{offer_sid: nil, state_sid: nil}}

  defp subscribe_state(state, user_id, @custom),
    do: Planes.subscribe(state.jetstream, self(), Subjects.overlay_state(user_id))

  defp subscribe_state(_state, _user_id, _accepts), do: {:ok, nil}

  defp queue_group(:tts, _template), do: "tts"
  defp queue_group(:overlay, template), do: "overlay.#{template}"

  defp watch_revocation(%{kind: :tts, session: session, rpc: rpc}),
    do: Planes.subscribe(rpc, self(), "controls.tts.revoked.#{session.user_id}")

  defp watch_revocation(_state), do: {:ok, nil}

  defp check(:overlay, session, _rpc) do
    with {:ok, overlay} <- Overlay.Nats.check(session) do
      {:ok,
       %{
         user_id: overlay.user_id,
         accepts: accepts(overlay.document),
         events: Feed.events(overlay.document),
         revision: overlay.revision
       }}
    end
  end

  defp accepts(%{"kind" => @custom}), do: @custom
  defp accepts(document), do: document["template"]

  defp check(:tts, session, rpc) do
    case Tts.Nats.check(session, rpc) do
      true -> {:ok, %{user_id: session.user_id, accepts: "tts", revision: nil}}
      false -> {:denied, :check_failed}
      :unavailable -> :unavailable
    end
  end

  defp start(req, headers, state) do
    state =
      Map.merge(state, %{
        monitor: Process.monitor(state.jetstream.pid),
        rpc_monitor: Process.monitor(state.rpc.pid),
        pending: Pending.start(self())
      })

    register_late(state)
    Metrics.count("Stream/Opened")
    Logger.info("#{label(state.kind)} events stream opened", log_id(state))

    req = :cowboy_req.stream_reply(200, event_headers(headers), req)
    initial = [initial_frame(@retry_ms), ready_frame(state)]
    :ok = :cowboy_req.stream_body(initial, :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    schedule_check(state)
    if state.state_sid, do: send(self(), :snapshot)
    {:cowboy_loop, req, state, :hibernate}
  end

  defp planes_missing(req, headers, %{kind: kind, session: session}) do
    Logger.warning("#{label(kind)} events planes unavailable", log_id(kind, session))
    headers = Map.put(headers, "retry-after", @planes_retry_after)
    {:ok, :cowboy_req.reply(503, headers, req), nil}
  end

  defp denied(req, headers, %{kind: kind, session: session}, reason) do
    Logger.info(
      "#{label(kind)} events link no longer valid",
      log_id(kind, session) ++ [reason: reason]
    )

    {:ok, :cowboy_req.reply(204, headers, req), nil}
  end

  defp unavailable(req, headers, %{kind: kind, session: session}, other) do
    Logger.warning(
      "#{label(kind)} events unavailable",
      log_id(kind, session) ++ [reason: inspect(other, limit: 5)]
    )

    body = initial_frame(@unavailable_retry_ms)

    {:ok, :cowboy_req.reply(200, event_headers(headers), body, req), nil}
  end

  def info({:msg, %{sid: sid, gnat: pid}}, req, %{revoked_sid: sid, rpc: %{pid: pid}} = state),
    do: stop(req, state, :revoked)

  def info(
        {:msg, %{sid: sid, gnat: pid, body: body}},
        req,
        %{jetstream: %{pid: pid}, state_sid: sid} = state
      ),
      do: forward_state(body, req, state)

  def info(
        {:msg, %{sid: sid, gnat: pid, reply_to: reply} = message},
        req,
        %{jetstream: %{pid: pid}} = state
      )
      when is_binary(reply) and sid == state.offer_sid,
      do: offer(message, req, state)

  def info({:DOWN, ref, :process, _pid, _reason}, req, state)
      when ref == state.monitor or ref == state.rpc_monitor,
      do: down(req, state, down_reason(ref, state))

  def info({:reconnect, after_ms, reason}, req, state),
    do: reconnect(req, state, {after_ms, reason}, env_ms(:stream_force_close_ms, @force_close_ms))

  def info({:force_close, reason}, req, state), do: stop(req, state, reason)

  def info(:snapshot, req, state) do
    case Feed.snapshot(state.user_id, state.events, state.rpc) do
      {:ok, snapshot} ->
        :ok = :cowboy_req.stream_body(feed_frame("snapshot", snapshot), :nofin, req)

      {:error, reason} ->
        Logger.warning("Overlay snapshot unavailable", log_id(state) ++ [reason: reason])
        Process.send_after(self(), :snapshot, recheck_ms())
    end

    continue(req, state)
  end

  def info(:ping, req, state) do
    :ok = :cowboy_req.stream_body(@ping_frame, :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    continue(req, state)
  end

  def info(:check, req, state) do
    verdict = verdict(state)
    reschedule(verdict, state)
    settle_verdict(verdict, req, state)
  end

  def info(:link_changed, req, state) do
    verdict = verdict(state)
    if verdict == :unavailable, do: Process.send_after(self(), :link_changed, recheck_ms())
    settle_verdict(verdict, req, state)
  end

  def info(_message, req, state), do: continue(req, state)

  defp reschedule({:ok, _link}, state), do: schedule_check(state)
  defp reschedule(:unavailable, state), do: schedule_recheck(state)
  defp reschedule({:denied, _reason}, _state), do: :ok

  defp settle_verdict({:ok, %{accepts: accepts} = link}, req, %{accepts: accepts} = state),
    do: announce(req, state, link.revision)

  defp settle_verdict({:ok, _link}, req, state) do
    after_ms = Platform.Backoff.between_ms(0, @reconnect_max_ms)

    reconnect(
      req,
      state,
      {after_ms, :template_changed},
      env_ms(:stream_force_close_ms, @force_close_ms)
    )
  end

  defp settle_verdict(:unavailable, req, state), do: continue(req, state)
  defp settle_verdict({:denied, reason}, req, state), do: stop(req, state, reason)

  defp down(req, state, :rpc_down) do
    close_ms = env_ms(:stream_force_close_ms, @force_close_ms)

    reconnect(
      req,
      state,
      {Platform.Backoff.between_ms(0, @reconnect_max_ms), :rpc_down},
      close_ms
    )
  end

  defp down(req, state, :nats_down) do
    after_ms = Platform.Backoff.between_ms(0, @reconnect_max_ms)
    close_ms = after_ms + env_ms(:stream_down_grace_ms, @down_grace_ms)
    reconnect(req, state, {after_ms, :nats_down}, close_ms)
  end

  defp reconnect(req, state, {after_ms, reason}, close_ms) do
    Metrics.count("Stream/Reconnect/#{reason}")
    announce_reconnect(req, state, after_ms)
    Process.send_after(self(), {:force_close, reason}, close_ms)
    continue(req, state)
  end

  defp announce_reconnect(req, state, after_ms) do
    frame = ["event: reconnect\ndata: {\"after_ms\": ", Integer.to_string(after_ms), "}\n\n"]
    Pending.handoff(state.pending)
    :ok = :cowboy_req.stream_body(frame, :nofin, req)
  end

  defp env_ms(key, default), do: Application.get_env(:controls_gateway, key, default)

  defp verdict(state) do
    if state.session.exp <= SignedCookie.now(),
      do: {:denied, :session_expired},
      else: check(state.kind, state.session, state.rpc)
  end

  defp offer(%{body: body, reply_to: reply}, req, state) do
    case Jason.decode(body) do
      {:ok, %{"kind" => kind} = offer} when kind == state.accepts ->
        accept(offer, reply, req, state)

      {:ok, %{"kind" => kind}} when is_binary(kind) ->
        continue(req, state)

      _ ->
        invalid(req, state)
    end
  end

  defp accept(%{"ack" => ack, "data" => %{} = data} = offer, reply, req, state)
       when is_binary(ack) do
    case event_id(data, state) do
      {:ok, id} -> deliver({offer, data, id}, reply, req, state)
      :error -> invalid(req, state)
    end
  end

  defp accept(_offer, _reply, req, state), do: invalid(req, state)

  defp deliver({offer, data, id}, reply, req, state) do
    :ok = :cowboy_req.stream_body(alert_frame(offer, data, id, state), :nofin, req)
    Planes.publish(state.jetstream, reply, @accepted)
    Pending.track(state.pending, offer["ack"])

    Logger.info(
      "#{label(state.kind)} event delivered",
      log_id(state) ++ [event_id: id, trace_id: data["trace_id"]]
    )

    continue(req, state)
  end

  defp event_id(%{"event_id" => id}, _state) when is_binary(id), do: {:ok, id}
  defp event_id(%{"id" => id}, %{accepts: @custom}) when is_binary(id), do: {:ok, id}
  defp event_id(_data, _state), do: :error

  defp invalid(req, state) do
    Logger.warning("#{label(state.kind)} offer invalid", log_id(state))
    continue(req, state)
  end

  defp alert_frame(offer, data, event_id, state) do
    alert = %{
      event_id: event_id,
      kind: offer["kind"],
      handle: Handle.sign(state.user_id, offer["ack"]),
      expires_at: expires_at(offer, data),
      data: data
    }

    [
      "id: ",
      String.replace(event_id, ["\r", "\n"], " "),
      "\nevent: alert\ndata: ",
      Jason.encode!(alert),
      "\n\n"
    ]
  end

  defp expires_at(offer, data) do
    Enum.find([data["expires_at"], offer["expires_at"]], &is_binary/1) ||
      Delivery.expires_at(DateTime.utc_now())
  end

  defp forward_state(body, req, state) do
    case Jason.decode(body) do
      {:ok, %{"type" => type, "data" => data}} when type in @state_frames ->
        :ok = :cowboy_req.stream_body(feed_frame(type, data), :nofin, req)

      _ ->
        Logger.warning("Overlay state invalid", log_id(state))
    end

    continue(req, state)
  end

  defp feed_frame(name, data), do: ["event: ", name, "\ndata: ", Jason.encode!(data), "\n\n"]

  defp ready_frame(state),
    do: ["event: ready\ndata: ", Jason.encode!(ready_data(state)), "\n\n"]

  defp ready_data(state) do
    %{now: System.os_time(:millisecond), key: overlay_key(state)}
    |> Map.merge(revision_field(state))
  end

  defp revision_field(%{revision: nil}), do: %{}
  defp revision_field(%{revision: revision}), do: %{revision: revision}

  defp overlay_key(state) do
    :crypto.hash(:sha256, "#{state.kind}:#{state.user_id}:#{scope(state)}")
    |> binary_part(0, 9)
    |> Base.url_encode64(padding: false)
  end

  defp scope(%{kind: :tts}), do: "tts"
  defp scope(%{session: session}), do: session.overlay_id

  defp announce(req, %{revision: revision} = state, revision), do: continue(req, state)

  defp announce(req, state, revision) do
    frame = ["event: revision\ndata: ", Integer.to_string(revision), "\n\n"]
    :ok = :cowboy_req.stream_body(frame, :nofin, req)
    continue(req, %{state | revision: revision})
  end

  defp down_reason(ref, %{monitor: ref}), do: :nats_down
  defp down_reason(_ref, _state), do: :rpc_down

  defp schedule_check(state),
    do: schedule(state, Application.get_env(:controls_gateway, :overlay_check_ms, @check_ms))

  defp schedule_recheck(state), do: schedule(state, recheck_ms())

  defp recheck_ms, do: Application.get_env(:controls_gateway, :overlay_recheck_ms, @recheck_ms)

  defp schedule(state, interval_ms) do
    delay = min(interval_ms, max(0, state.session.exp - SignedCookie.now()) * 1_000)
    Process.send_after(self(), :check, delay)
  end

  defp initial_frame(retry_ms), do: "retry: #{Enum.random(retry_ms)}\n" <> @ping_frame
  defp event_headers(headers), do: Map.put(headers, "content-type", "text/event-stream")
  defp continue(req, state), do: {:ok, req, state, :hibernate}

  defp label(kind), do: Map.fetch!(@labels, kind)
  defp log_id(%{kind: kind, session: session}), do: log_id(kind, session)
  defp log_id(:tts, session), do: [user_id: session.user_id]
  defp log_id(:overlay, session), do: [overlay_id: session.overlay_id]

  defp stop(req, state, reason) do
    Metrics.count("Stream/Closed/#{reason}")
    log_stop(reason, state)
    :cowboy_req.stream_body("", :fin, req)
    {:stop, req, state}
  end

  defp log_stop(reason, state) when reason in [:drain, :revoked],
    do:
      Logger.info("#{label(state.kind)} events stream closed", log_id(state) ++ [reason: reason])

  defp log_stop(reason, state),
    do:
      Logger.warning(
        "#{label(state.kind)} events stream closed",
        log_id(state) ++ [reason: reason]
      )
end
