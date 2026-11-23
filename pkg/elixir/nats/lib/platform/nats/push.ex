# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.Push do
  @moduledoc "Ephemeral JetStream push consumer with flow control, gap check and start-at-sequence resume."

  alias Platform.Nats.Planes

  @heartbeat_ns 5_000_000_000
  @idle_ns 30_000_000_000
  @stale_ms 15_000
  @request_timeout_ms 2_000
  @max_resume_id 18_446_744_073_709_551_614

  @enforce_keys [:jetstream, :stream, :subject, :inbox, :sid, :name, :start]
  defstruct [:jetstream, :stream, :subject, :inbox, :sid, :name, :start, :consumer, seq: 0, seen_at: 0]

  @type start :: :new | {:sequence, pos_integer()}

  @spec open(Planes.t(), String.t(), String.t(), start(), String.t()) ::
          {:ok, %__MODULE__{}} | {:error, term()}
  def open(%Planes{} = jetstream, stream, subject, start, prefix) do
    id = Base.url_encode64(:crypto.strong_rand_bytes(18), padding: false)
    inbox = "_INBOX." <> prefix <> "." <> id

    with {:ok, sid} <- Planes.subscribe(jetstream, self(), inbox) do
      {:ok,
       %__MODULE__{
         jetstream: jetstream,
         stream: stream,
         subject: subject,
         inbox: inbox,
         sid: sid,
         name: prefix <> "-" <> id,
         start: start,
         seen_at: now()
       }}
    end
  end

  @spec attach(%__MODULE__{}) :: {:ok, %__MODULE__{}} | {:error, term()}
  def attach(push) do
    api = "$JS.API.CONSUMER.CREATE.#{push.stream}.#{push.name}.#{push.subject}"

    config =
      Map.merge(
        %{
          name: push.name,
          deliver_subject: push.inbox,
          filter_subject: push.subject,
          ack_policy: "none",
          max_deliver: 1,
          idle_heartbeat: @heartbeat_ns,
          inactive_threshold: @idle_ns,
          flow_control: true
        },
        delivery_policy(push.start)
      )

    case request(push.jetstream, api, %{stream_name: push.stream, config: config}) do
      {:ok, %{"name" => name}} when name == push.name ->
        {:ok, %{push | consumer: name, seen_at: now()}}

      other ->
        {:error, other}
    end
  end

  @spec stream_range(Planes.t(), String.t()) :: {:ok, non_neg_integer(), non_neg_integer()} | :error
  def stream_range(jetstream, stream) do
    case request(jetstream, "$JS.API.STREAM.INFO.#{stream}", %{}) do
      {:ok, %{"state" => %{"first_seq" => first, "last_seq" => last}}} -> {:ok, first, last}
      _ -> :error
    end
  end

  @spec owns?(%__MODULE__{}, map()) :: boolean()
  def owns?(push, %{sid: sid, gnat: pid}), do: sid == push.sid and pid == push.jetstream.pid

  @spec handle(%__MODULE__{}, map()) ::
          {:ok, %__MODULE__{}}
          | {:event, %{seq: pos_integer(), topic: String.t(), body: binary()}, %__MODULE__{}}
          | {:error, :flow_gap | :out_of_order}
  def handle(push, %{status: "100"} = message) do
    headers = Map.new(Map.get(message, :headers, []))
    last = Map.get(headers, "nats-last-consumer", Integer.to_string(push.seq))
    reply = message.reply_to || Map.get(headers, "nats-consumer-stalled")

    if reply, do: Planes.publish(push.jetstream, reply, "")

    if last == Integer.to_string(push.seq),
      do: {:ok, %{push | seen_at: now()}},
      else: {:error, :flow_gap}
  end

  def handle(push, %{topic: topic, body: body} = message) do
    next = push.seq + 1

    case Planes.jetstream_metadata(message) do
      {:ok, %{consumer: consumer, stream_seq: stream_seq, consumer_seq: ^next}}
      when consumer == push.consumer ->
        {:event, %{seq: stream_seq, topic: topic, body: body},
         %{push | seq: next, seen_at: now()}}

      _ ->
        {:error, :out_of_order}
    end
  end

  @spec tick(%__MODULE__{}) :: {:ok, %__MODULE__{}} | :stale | {:error, term()}
  def tick(%{consumer: nil} = push), do: attach(push)

  def tick(push) do
    if now() - push.seen_at < @stale_ms, do: {:ok, push}, else: :stale
  end

  @doc "Turns a Last-Event-ID style stream sequence into the sequence to start at."
  @spec resume_sequence(term()) :: pos_integer() | nil
  def resume_sequence(id) when is_binary(id) do
    case Integer.parse(id) do
      {seq, ""} when seq >= 1 and seq <= @max_resume_id -> seq + 1
      _ -> nil
    end
  end

  def resume_sequence(_id), do: nil

  defp request(jetstream, subject, body) do
    message = {subject, Jason.encode!(body), [receive_timeout: @request_timeout_ms]}

    with {:ok, %{body: json}} <- Planes.jetstream_request(message, jetstream) do
      Jason.decode(json)
    end
  end

  defp delivery_policy(:new), do: %{deliver_policy: "new"}

  defp delivery_policy({:sequence, seq}),
    do: %{deliver_policy: "by_start_sequence", opt_start_seq: seq}

  defp now, do: System.monotonic_time(:millisecond)
end
