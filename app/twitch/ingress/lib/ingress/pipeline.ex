# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Pipeline do
  require Logger

  alias Ingress.{BroadcasterCache, Config, JSON, LaneMessage, Metrics, Nats, Squash, Trace}

  @type decision :: :special | :command | :chat

  @type lane :: :premium | :standard | :stream | :drop

  @stream_types ["stream.online", "stream.offline"]

  def handle_event(payload, meta) do
    decision =
      Trace.span("route", fn ->
        decision = route(payload, meta)

        Trace.add_span_attributes(
          result: Trace.result(decision),
          "event.lane": decision_lane(decision)
        )

        decision
      end)

    case decision do
      {:publish, subject, message} ->
        publish_one(subject, message)

      {:publish_many, publishes} ->
        Enum.each(publishes, fn {subject, message} -> publish_one(subject, message) end)

      :squash ->
        Metrics.count("Squashed")
        :squash

      :oversized ->
        Metrics.count("Oversized")
        :oversized

      :drop ->
        Metrics.count("Dropped")
        :drop
    end
  end

  defp decision_lane({:publish, _subject, %{lane: lane}}), do: to_string(lane)

  defp decision_lane({:publish_many, [{_subject, %{lane: lane}} | _]}),
    do: to_string(lane)

  defp decision_lane(_decision), do: "none"

  defp publish_one(subject, message) do
    Metrics.count("Published/#{message.lane}")
    Nats.publish_acked(subject, message)
  end

  @spec route(map(), map()) ::
          {:publish, String.t(), map()} | {:publish_many, [{String.t(), map()}]} | :drop
  def route(payload, meta), do: do_route(payload, meta, Config.hot_path())

  defp do_route(%{"subscription" => %{"type" => type}, "event" => event}, meta, hot)
       when type in @stream_types do
    event_lane = event_lane(event)

    body =
      JSON.members(%{
        type: type,
        event: event,
        shard_id: meta.shard_id,
        msg_id: meta.msg_id,
        received_at: meta.ts
      })

    publishes =
      [{hot.lane_subjects.stream, %LaneMessage{lane: :stream, body: body}}] ++
        case event_lane do
          :drop -> []
          lane -> [{Map.fetch!(hot.lane_subjects, lane), %LaneMessage{lane: lane, body: body}}]
        end

    {:publish_many, publishes}
  end

  defp do_route(
         %{"subscription" => %{"type" => "channel.chat.message"}, "event" => event},
         meta,
         hot
       ) do
    text = get_in(event, ["message", "text"]) || ""

    cond do
      byte_size(text) > hot.max_chat_text_bytes ->
        :oversized

      true ->
        case decide(text, event["chatter_user_id"], hot.special_user_ids) do
          :special -> chat_message(:premium, event, text, meta, hot)
          :command -> broadcaster_lane_publish(event, text, meta, hot)
          :chat -> plain_chat(event, text, meta, hot)
        end
    end
  end

  defp do_route(%{"subscription" => %{"type" => type}, "event" => event}, meta, hot) do
    lane = event_lane(event)

    case lane do
      :drop ->
        :drop

      lane ->
        {:publish, Map.fetch!(hot.lane_subjects, lane),
         %{
           type: type,
           lane: lane,
           event: event,
           shard_id: meta.shard_id,
           msg_id: meta.msg_id,
           received_at: meta.ts
         }}
    end
  end

  defp do_route(payload, _meta, _hot) do
    Logger.warning("notification without subscription/event: #{inspect(payload)}")
    :drop
  end

  @spec decide(String.t(), String.t() | nil, Enumerable.t()) :: decision()
  def decide(text, chatter_id, special_user_ids) do
    cond do
      chatter_id != nil and chatter_id in special_user_ids -> :special
      String.starts_with?(String.trim_leading(text), "!") -> :command
      true -> :chat
    end
  end

  defp event_lane(event) do
    case broadcaster_id(event) do
      nil -> :standard
      id -> BroadcasterCache.lane(id)
    end
  end

  @spec broadcaster_id(map()) :: String.t() | nil
  def broadcaster_id(event) do
    event["broadcaster_user_id"] || event["to_broadcaster_user_id"]
  end

  defp broadcaster_lane_publish(event, text, meta, hot) do
    case BroadcasterCache.lane(event["broadcaster_user_id"]) do
      :drop -> :drop
      lane -> chat_message(lane, event, text, meta, hot)
    end
  end

  defp plain_chat(event, text, meta, hot) do
    case BroadcasterCache.lane(event["broadcaster_user_id"]) do
      :drop ->
        :drop

      lane ->
        case Squash.observe_chat(lane, event, text, meta) do
          :buffered -> :squash
          :first -> chat_message(lane, event, text, meta, hot)
        end
    end
  end

  defp chat_message(lane, event, text, meta, hot) do
    subject = Map.fetch!(hot.lane_subjects, lane)

    message = %{
      type: "channel.chat.message",
      lane: lane,
      broadcaster_user_id: event["broadcaster_user_id"],
      broadcaster_user_login: event["broadcaster_user_login"],
      broadcaster_user_name: event["broadcaster_user_name"],
      chatter_user_id: event["chatter_user_id"],
      chatter_user_login: event["chatter_user_login"],
      chatter_user_name: event["chatter_user_name"],
      text: text,
      badges: event["badges"],
      msg_id: meta.msg_id,
      event_id: meta.msg_id,
      chat_message_id: event["message_id"],
      shard_id: meta.shard_id,
      ts: meta.ts,
      received_at: meta.ts
    }

    message =
      case Map.get(meta, :origin) do
        :trial ->
          message
          |> Map.put(:origin, "trial")
          |> Map.put(:trial_generation, Map.fetch!(meta, :trial_generation))

        _ ->
          message
      end

    case emote_spans(event) do
      [] -> {:publish, subject, message}
      spans -> {:publish, subject, Map.put(message, :emotes, spans)}
    end
  end

  @emote_fragment_types ["emote", "cheermote"]

  @spec emote_spans(map()) :: [
          %{id: String.t() | nil, begin: non_neg_integer(), end: non_neg_integer()}
        ]
  def emote_spans(%{"message" => %{"fragments" => fragments}}) when is_list(fragments) do
    {spans, _offset} =
      Enum.flat_map_reduce(fragments, 0, fn fragment, offset ->
        width = codepoint_width(fragment["text"])

        spans =
          if fragment["type"] in @emote_fragment_types do
            [%{id: fragment_id(fragment), begin: offset, end: offset + width}]
          else
            []
          end

        {spans, offset + width}
      end)

    spans
  end

  def emote_spans(_event), do: []

  defp codepoint_width(text) when is_binary(text),
    do: for(<<_::utf8 <- text>>, reduce: 0, do: (n -> n + 1))

  defp codepoint_width(_text), do: 0

  defp fragment_id(%{"emote" => %{"id" => id}}) when is_binary(id), do: id
  defp fragment_id(%{"cheermote" => %{"prefix" => prefix}}) when is_binary(prefix), do: prefix
  defp fragment_id(_fragment), do: nil
end
