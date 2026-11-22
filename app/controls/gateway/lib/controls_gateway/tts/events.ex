# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Events do
  require Logger

  alias ControlsGateway.Tts.{Nats, Session}
  alias Platform.Nats.Planes

  @stream "OVERLAY_TTS"
  @ping_ms 15_000
  @check_ms 300_000
  @heartbeat_ns 5_000_000_000
  @idle_ns 30_000_000_000
  @max_resume_id 18_446_744_073_709_551_614
  @retry_ms 3_000..8_000
  @unavailable_retry_ms 15_000..30_000
  @ping_frame ": ping\n\n"

  def open(req, session, headers) do
    with %Planes{} = jetstream <- Planes.connection(:jetstream),
         %Planes{} = rpc <- Planes.connection(:rpc),
         {:ok, revoked_sid} <-
           Planes.subscribe(rpc, self(), "controls.tts.revoked.#{session.user_id}"),
         true <- Nats.check(session, rpc),
         inbox <-
           "_INBOX.tts." <> Base.url_encode64(:crypto.strong_rand_bytes(18), padding: false),
         {:ok, event_sid} <- Planes.subscribe(jetstream, self(), inbox) do
      state = %{
        jetstream: jetstream,
        rpc: rpc,
        monitor: Process.monitor(jetstream.pid),
        rpc_monitor: Process.monitor(rpc.pid),
        session: session,
        revoked_sid: revoked_sid,
        event_sid: event_sid,
        inbox: inbox,
        subject: "overlay.tts.#{session.user_id}",
        resume: req |> resume_id() |> resume_sequence(),
        consumer: nil,
        seq: 0,
        seen_at: now()
      }

      start(req, headers, state)
    else
      false ->
        {:ok, :cowboy_req.reply(204, headers, req), nil}

      _ ->
        body = initial_frame(@unavailable_retry_ms)
        {:ok, :cowboy_req.reply(200, event_headers(headers), body, req), nil}
    end
  end

  defp start(req, headers, state) do
    state = attach(state)
    req = :cowboy_req.stream_reply(200, event_headers(headers), req)
    :ok = :cowboy_req.stream_body(initial_frame(@retry_ms), :nofin, req)
    Process.send_after(self(), :ping, @ping_ms)
    schedule_check(state.session)
    {:cowboy_loop, req, state, :hibernate}
  end

  defp initial_frame(retry_ms), do: "retry: #{Enum.random(retry_ms)}\n" <> @ping_frame

  defp attach(state) do
    name = "tts-" <> List.last(String.split(state.inbox, "."))
    subject = "$JS.API.CONSUMER.CREATE.#{@stream}.#{name}.#{state.subject}"

    config = %{
      name: name,
      deliver_subject: state.inbox,
      filter_subject: state.subject,
      ack_policy: "none",
      max_deliver: 1,
      idle_heartbeat: @heartbeat_ns,
      inactive_threshold: @idle_ns,
      flow_control: true
    }

    config = Map.merge(config, delivery_policy(state.resume))

    case Nats.jetstream_request(state.jetstream, subject, %{stream_name: @stream, config: config}) do
      {:ok, %{"name" => ^name}} ->
        %{state | consumer: name, seen_at: now()}

      other ->
        Logger.warning("TTS events consumer unavailable",
          user_id: state.session.user_id,
          reason: inspect(other, limit: 5)
        )

        state
    end
  end

  def info({:msg, %{sid: sid, gnat: rpc}}, req, %{revoked_sid: sid, rpc: %{pid: rpc}} = state),
    do: stop(req, state)

  def info({:msg, %{sid: sid, gnat: pid} = message}, req, state)
      when sid == state.event_sid and pid == state.jetstream.pid,
      do: deliver(message, req, state)

  def info({:DOWN, ref, :process, _pid, _reason}, req, state)
      when ref == state.monitor or ref == state.rpc_monitor,
      do: stop(req, state)

  def info(:ping, req, state) do
    case ping(state) do
      :stale ->
        stop(req, state)

      state ->
        :ok = :cowboy_req.stream_body(@ping_frame, :nofin, req)
        Process.send_after(self(), :ping, @ping_ms)
        continue(req, state)
    end
  end

  def info(:check, req, state) do
    if valid?(state) do
      schedule_check(state.session)
      continue(req, state)
    else
      stop(req, state)
    end
  end

  def info(:shutdown_close, req, state), do: stop(req, state)

  def info(_message, req, state), do: continue(req, state)

  defp deliver(%{status: "100"} = message, req, state) do
    headers = Map.new(Map.get(message, :headers, []))
    last = Map.get(headers, "nats-last-consumer", Integer.to_string(state.seq))
    reply = message.reply_to || Map.get(headers, "nats-consumer-stalled")

    if reply, do: Planes.publish(state.jetstream, reply, "")

    if last == Integer.to_string(state.seq) do
      continue(req, %{state | seen_at: now()})
    else
      stop(req, state)
    end
  end

  defp deliver(%{topic: topic, body: body} = message, req, %{subject: topic} = state) do
    next = state.seq + 1

    case Planes.jetstream_metadata(message) do
      {:ok, %{consumer: consumer, stream_seq: stream_seq, consumer_seq: ^next}}
      when consumer == state.consumer ->
        send_event(req, stream_seq, body, state.session)
        continue(req, %{state | seq: next, seen_at: now()})

      _ ->
        stop(req, state)
    end
  end

  defp deliver(_message, req, state), do: stop(req, state)

  defp send_event(req, seq, body, session) do
    case Jason.decode(body) do
      {:ok, event} when is_map(event) ->
        :cowboy_req.stream_body(
          ["id: ", Integer.to_string(seq), "\ndata: ", Jason.encode!(event), "\n\n"],
          :nofin,
          req
        )

        Logger.info("TTS event delivered",
          user_id: session.user_id,
          event_id: event["event_id"],
          trace_id: event["trace_id"]
        )

      _ ->
        Logger.warning("TTS event payload invalid", user_id: session.user_id)
    end
  end

  defp ping(%{consumer: nil} = state), do: attach(state)

  defp ping(state) do
    if now() - state.seen_at < @ping_ms, do: state, else: :stale
  end

  defp valid?(state),
    do: state.session.exp > Session.now() and Nats.check(state.session, state.rpc) == true

  defp schedule_check(session) do
    delay = min(@check_ms, max(0, session.exp - Session.now()) * 1_000)
    Process.send_after(self(), :check, delay)
  end

  defp resume_id(req) do
    case :cowboy_req.header("last-event-id", req) do
      :undefined ->
        req |> :cowboy_req.parse_qs() |> List.keyfind("after", 0, {"after", ""}) |> elem(1)

      id ->
        id
    end
  end

  defp resume_sequence(id) when is_binary(id) do
    case Integer.parse(id) do
      {seq, ""} when seq >= 1 and seq <= @max_resume_id -> seq + 1
      _ -> nil
    end
  end

  defp resume_sequence(_id), do: nil

  defp delivery_policy(nil), do: %{deliver_policy: "new"}
  defp delivery_policy(seq), do: %{deliver_policy: "by_start_sequence", opt_start_seq: seq}
  defp event_headers(headers), do: Map.put(headers, "content-type", "text/event-stream")
  defp now, do: System.monotonic_time(:millisecond)
  defp continue(req, state), do: {:ok, req, state, :hibernate}

  defp stop(req, state) do
    :cowboy_req.stream_body("", :fin, req)
    {:stop, req, state}
  end
end
