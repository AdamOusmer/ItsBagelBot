defmodule Ingress.Pipeline do
  @moduledoc """
  Decides what happens to each EventSub notification.

  There are exactly three lane subjects:

    * `twitch.ingress.event.premium` / `twitch.ingress.event.standard`: all
      events, laned by broadcaster status.
    * `twitch.ingress.event.stream`: the live lane, carrying `stream.online`
      and `stream.offline` regardless of broadcaster status.

  Live events are **dual-published**: every `stream.online`/`stream.offline`
  goes to the live lane *and* to the broadcaster's own event lane
  (premium/standard), so a consumer draining chat/follows/subs/cheers also sees
  the channel go live without subscribing to the live lane.

  Every published event carries its `type` in the payload so consumers filter
  there, not on the subject.

  For `channel.chat.message` there are three outcomes:

    1. The chatter is one of the special user IDs (from secrets): publish to
       the **premium** lane, always, even when the broadcaster is on the free
       tier.
    2. The message text starts with `!` (a command): publish to the lane
       matching the **broadcaster's** status, looked up through
       `Ingress.BroadcasterCache`. Commands are never squashed - a repeated
       command is a legitimate second invocation the worker gates by cooldown.
    3. Anything else (plain chat): published to the broadcaster's lane so the
       worker's automod sees every message. Identical lines are coalesced by
       `Ingress.Squash` into one folded `channel.chat.message` carrying every
       sender, so per-user reputation and cross-user campaign detection keep the
       full signal at a fraction of the event count.

  A size guard drops oversized/malformed chat text (`max_chat_text_bytes`)
  before any routing.

  Every other EventSub type rides the premium/standard lanes, routed by the
  event's broadcaster status. Events without an extractable broadcaster
  default to the standard lane.
  """

  require Logger

  alias Ingress.{BroadcasterCache, Config, Metrics, Nats, Squash}

  @type decision :: :special | :command | :chat

  @type lane :: :premium | :standard | :stream | :drop

  @stream_types ["stream.online", "stream.offline"]

  def handle_event(payload, meta) do
    case route(payload, meta) do
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

  defp publish_one(subject, message) do
    Metrics.count("Published/#{message.lane}")
    Nats.publish(subject, message)
  end

  @doc """
  Pure-ish routing (the only side effect is the broadcaster cache read).
  Returns the subject and payload to publish, a list of them, or `:drop`.
  """
  @spec route(map(), map()) ::
          {:publish, String.t(), map()} | {:publish_many, [{String.t(), map()}]} | :drop
  def route(%{"subscription" => %{"type" => type}, "event" => event}, meta)
      when type in @stream_types do
    # A live event rides two lanes at once: the dedicated stream (live) lane,
    # and the broadcaster's own event lane (premium/standard) so consumers
    # watching chat/follows/subs/cheers also see the channel go live without
    # subscribing to the live lane. The stream lane is unconditional; the event
    # lane is whatever the broadcaster's status resolves to.
    event_lane =
      case broadcaster_id(event) do
        nil -> :standard
        id -> BroadcasterCache.lane(id)
      end

    # The stream lane is unconditional. The broadcaster's own event lane is
    # added only when they are not dropped (banned): a banned broadcaster's
    # live event still rides the stream lane but never their event lane.
    publishes =
      [{Config.lane_subject(:stream), stream_message(:stream, type, event, meta)}] ++
        case event_lane do
          :drop -> []
          lane -> [{Config.lane_subject(lane), stream_message(lane, type, event, meta)}]
        end

    {:publish_many, publishes}
  end

  def route(%{"subscription" => %{"type" => "channel.chat.message"}, "event" => event}, meta) do
    text = get_in(event, ["message", "text"]) || ""

    cond do
      # Size guard: a well-formed Twitch chat line is <= 500 chars; anything far
      # past that is malformed or abuse and is dropped before any further work.
      oversized?(text) ->
        :oversized

      true ->
        case decide(text, event["chatter_user_id"], Config.special_user_ids()) do
          :special -> chat_message(:premium, event, text, meta)
          :command -> broadcaster_lane_publish(event, text, meta)
          :chat -> plain_chat(event, text, meta)
        end
    end
  end

  def route(%{"subscription" => %{"type" => type}, "event" => event}, meta) do
    lane =
      case broadcaster_id(event) do
        nil -> :standard
        id -> BroadcasterCache.lane(id)
      end

    case lane do
      :drop ->
        :drop

      lane ->
        {:publish, Config.lane_subject(lane),
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

  def route(payload, _meta) do
    Logger.warning("notification without subscription/event: #{inspect(payload)}")
    :drop
  end

  @doc """
  Pure decision for a chat message. The special-user check wins over the
  command check, because special users go premium unconditionally.
  """
  @spec decide(String.t(), String.t() | nil, Enumerable.t()) :: decision()
  def decide(text, chatter_id, special_user_ids) do
    cond do
      chatter_id != nil and chatter_id in special_user_ids -> :special
      String.starts_with?(String.trim_leading(text), "!") -> :command
      true -> :chat
    end
  end

  @doc """
  The broadcaster whose channel an event belongs to. Most channel events carry
  `broadcaster_user_id`; inbound raids identify the receiving channel as
  `to_broadcaster_user_id`.
  """
  @spec broadcaster_id(map()) :: String.t() | nil
  def broadcaster_id(event) do
    event["broadcaster_user_id"] || event["to_broadcaster_user_id"]
  end

  defp stream_message(lane, type, event, meta) do
    %{
      type: type,
      lane: lane,
      event: event,
      shard_id: meta.shard_id,
      msg_id: meta.msg_id,
      received_at: meta.ts
    }
  end

  # Commands (and their like): publish to the broadcaster's own lane, unless the
  # broadcaster is dropped (banned). Never squashed or shed, so a legitimate
  # repeated command still runs (the worker gates abuse by cooldown).
  defp broadcaster_lane_publish(event, text, meta) do
    case BroadcasterCache.lane(event["broadcaster_user_id"]) do
      :drop -> :drop
      lane -> chat_message(lane, event, text, meta)
    end
  end

  # Plain (non-command) chat now flows to the worker for the automod. Identical
  # lines are coalesced by Ingress.Squash: the first publishes immediately, the
  # rest fold into one channel.chat.message carrying every sender.
  defp plain_chat(event, text, meta) do
    case BroadcasterCache.lane(event["broadcaster_user_id"]) do
      :drop ->
        :drop

      lane ->
        case Squash.observe(cohort_base(lane, event, text), sender_entry(event, meta)) do
          :buffered -> :squash
          :first -> chat_message(lane, event, text, meta)
        end
    end
  end

  defp oversized?(text), do: byte_size(text) > Config.max_chat_text_bytes()

  defp cohort_base(lane, event, text) do
    %{
      broadcaster_user_id: event["broadcaster_user_id"],
      broadcaster_user_login: event["broadcaster_user_login"],
      lane: lane,
      text: text
    }
  end

  defp sender_entry(event, meta) do
    %{
      chatter_user_id: event["chatter_user_id"],
      chatter_user_login: event["chatter_user_login"],
      msg_id: meta.msg_id,
      ts: meta.ts,
      badges: event["badges"]
    }
  end

  defp chat_message(lane, event, text, meta) do
    {:publish, Config.lane_subject(lane),
     %{
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
       shard_id: meta.shard_id,
       ts: meta.ts
     }}
  end
end
