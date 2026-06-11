defmodule Ingress.Pipeline do
  @moduledoc """
  Decides what happens to each EventSub notification.

  There are exactly three lane subjects:

    * `twitch.ingress.event.premium` / `twitch.ingress.event.standard`: all
      events, laned by broadcaster status.
    * `twitch.ingress.event.stream`: **only** `stream.online` and
      `stream.offline`, regardless of broadcaster status.

  Every published event carries its `type` in the payload so consumers filter
  there, not on the subject.

  For `channel.chat.message` there are exactly three outcomes:

    1. The chatter is one of the special user IDs (from secrets): publish to
       the **premium** lane, always, even when the broadcaster is on the free
       tier.
    2. The message text starts with `!`: publish to the lane matching the
       **broadcaster's** status (premium or standard), looked up through
       `Ingress.BroadcasterCache`.
    3. Anything else: drop.

  Every other EventSub type rides the premium/standard lanes, routed by the
  event's broadcaster status. Events without an extractable broadcaster
  default to the standard lane.
  """

  require Logger

  alias Ingress.{BroadcasterCache, Config, Metrics, Nats}

  @type decision :: :special | :command | :drop

  @stream_types ["stream.online", "stream.offline"]

  def handle_event(payload, meta) do
    case route(payload, meta) do
      {:publish, subject, message} ->
        Metrics.count("Published/#{message.lane}")
        Nats.publish(subject, message)

      :drop ->
        Metrics.count("Dropped")
        :drop
    end
  end

  @doc """
  Pure-ish routing (the only side effect is the broadcaster cache read).
  Returns the subject and payload to publish, or `:drop`.
  """
  @spec route(map(), map()) :: {:publish, String.t(), map()} | :drop
  def route(%{"subscription" => %{"type" => type}, "event" => event}, meta)
      when type in @stream_types do
    {:publish, Config.lane_subject(:stream),
     %{
       type: type,
       lane: :stream,
       event: event,
       shard_id: meta.shard_id,
       msg_id: meta.msg_id,
       received_at: meta.ts
     }}
  end

  def route(%{"subscription" => %{"type" => "channel.chat.message"}, "event" => event}, meta) do
    text = get_in(event, ["message", "text"]) || ""

    case decide(text, event["chatter_user_id"], Config.special_user_ids()) do
      :drop ->
        :drop

      :special ->
        chat_message(:premium, event, text, meta)

      :command ->
        chat_message(BroadcasterCache.lane(event["broadcaster_user_id"]), event, text, meta)
    end
  end

  def route(%{"subscription" => %{"type" => type}, "event" => event}, meta) do
    lane =
      case broadcaster_id(event) do
        nil -> :standard
        id -> BroadcasterCache.lane(id)
      end

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
      true -> :drop
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

  defp chat_message(lane, event, text, meta) do
    {:publish, Config.lane_subject(lane),
     %{
       type: "channel.chat.message",
       lane: lane,
       broadcaster_user_id: event["broadcaster_user_id"],
       broadcaster_user_login: event["broadcaster_user_login"],
       chatter_user_id: event["chatter_user_id"],
       chatter_user_login: event["chatter_user_login"],
       text: text,
       msg_id: meta.msg_id,
       shard_id: meta.shard_id,
       ts: meta.ts
     }}
  end
end
