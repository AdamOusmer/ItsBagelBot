# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.Pipeline do
  @moduledoc """
  Normalization and lane routing for streamList chat events — the YouTube
  counterpart of the Twitch ingress filter, kept semantically aligned so
  downstream consumers can treat both platforms uniformly:

    1. Chat messages (`text_message_event`) have exactly three outcomes:
       authors from the special-user list always go to the **premium** lane;
       messages starting with `!` go to the lane matching the **broadcaster's**
       status (premium or standard); everything else is dropped.

    2. Paid/membership/poll events (`super_chat_event`, `super_sticker_event`,
       `new_sponsor_event`, `member_milestone_chat_event`,
       `membership_gifting_event`, `gift_membership_received_event`,
       `poll_event`, `gift_event`) are non-chat signals: they publish to the
       broadcaster's own lane so consumers can react without parsing chat.

    3. Everything else (`tombstone`, `chat_ended_event`, moderation notices,
       sponsor-only mode flips, legacy fan funding) is not actionable
       downstream today and is dropped with a counter.

  Event payloads mirror the Twitch ingress wire shape — `{type, lane,
  broadcaster_user_id, ..., ts, msg_id}` — plus `"platform": "youtube"` and
  YouTube-native extras documented per type below. Consumers de-duplicate on
  `msg_id`: reconnects resume from the last `next_page_token`, but a cold
  start replays a bounded recent-history window exactly once.
  """

  alias YtIngress.{BroadcasterCache, Config, Metrics, Nats}
  alias Youtube.Api.V3.LiveChatMessage

  @command_prefix "!"

  # Non-chat types that publish to the broadcaster's lane. Anything else that
  # arrives is dropped by handle_message/2.
  @published_non_chat [
    "super_chat_event",
    "super_sticker_event",
    "new_sponsor_event",
    "member_milestone_chat_event",
    "membership_gifting_event",
    "gift_membership_received_event",
    "poll_event",
    "gift_event"
  ]

  @doc """
  Routes one decoded streamList message onto its lane subject. Returns
  `{:ok, subject}` when published, `{:dropped, reason}` otherwise; never
  raises into the caller.

  The publisher and lane lookup are injectable so tests exercise the routing
  without NATS or the broadcaster cache (same pattern as the cache's
  injectable loader).
  """
  @spec handle_message(LiveChatMessage.t(), String.t(), keyword()) ::
          {:ok, String.t()} | {:dropped, atom()}
  def handle_message(%LiveChatMessage{} = message, broadcaster_id, opts \\ []) do
    publisher = Keyword.get(opts, :publisher, &Nats.publish_event/2)
    lane_lookup = Keyword.get(opts, :lane_lookup, &BroadcasterCache.lane/1)

    case type_string(message) do
      "text_message_event" ->
        handle_text(message, broadcaster_id, publisher, lane_lookup)

      type when type in @published_non_chat ->
        handle_non_chat(message, broadcaster_id, type, publisher, lane_lookup)

      _other ->
        Metrics.count("Filter/Dropped")
        {:dropped, :not_actionable}
    end
  end

  # --- text messages ---------------------------------------------------------

  defp handle_text(message, broadcaster_id, publisher, lane_lookup) do
    author = message.author_details || %LiveChatMessage{}
    snippet = message.snippet
    text = snippet && snippet.display_message
    author_id = author.channel_id || (snippet && snippet.author_channel_id)

    cond do
      blank?(author_id) ->
        drop(:no_author)

      special_user?(author_id) ->
        publish_text(message, broadcaster_id, :premium, author_id, text, publisher)

      command?(text) ->
        case lane_lookup.(broadcaster_id) do
          :drop -> drop(:broadcaster_banned)
          lane -> publish_text(message, broadcaster_id, lane, author_id, text, publisher)
        end

      true ->
        # Plain chatter: same filter philosophy as Twitch — only commands and
        # special authors cross the bus.
        drop(:plain_chat)
    end
  end

  defp publish_text(message, broadcaster_id, lane, author_id, text, publisher) do
    if oversized?(text) do
      drop(:oversized)
    else
      author = message.author_details
      display_name = author && author.display_name

      event =
        base_event("text_message_event", broadcaster_id, lane, message)
        |> Map.put(:chatter_user_id, author_id)
        |> put_unless_nil(:chatter_user_login, login(display_name))
        |> put_unless_nil(:chatter_user_name, display_name)
        |> put_unless_nil(:text, text)
        |> put_flags(author)

      publish(publisher, lane_subject(lane), event)
    end
  end

  # --- paid / membership / poll events ---------------------------------------

  defp handle_non_chat(message, broadcaster_id, type, publisher, lane_lookup) do
    case lane_lookup.(broadcaster_id) do
      :drop ->
        drop(:broadcaster_banned)

      lane ->
        event =
          base_event(type, broadcaster_id, lane, message)
          |> Map.merge(details(type, message.snippet))

        publish(publisher, lane_subject(lane), event)
    end
  end

  # protobuf-elixir stores oneof members as `{field_name, value}` under the
  # parent oneof key (`displayed_content`), so detail extraction dispatches on
  # that tuple rather than on the message type string — the content is the
  # truth even when a future YouTube type renames.
  defp details(_type, %{displayed_content: {:super_chat_details, d}}) when not is_nil(d),
    do: %{
      amount_micros: d.amount_micros,
      currency: d.currency,
      amount_display_string: d.amount_display_string,
      tier: d.tier,
      user_comment: d.user_comment
    }

  defp details(_type, %{displayed_content: {:super_sticker_details, d}}) when not is_nil(d),
    do: %{
      amount_micros: d.amount_micros,
      currency: d.currency,
      amount_display_string: d.amount_display_string,
      tier: d.tier,
      sticker_id: d.super_sticker_metadata && d.super_sticker_metadata.sticker_id
    }

  defp details(_type, %{displayed_content: {:new_sponsor_details, d}}) when not is_nil(d),
    do: %{member_level_name: d.member_level_name, is_upgrade: d.is_upgrade}

  defp details(_type, %{displayed_content: {:member_milestone_chat_details, d}})
       when not is_nil(d),
    do: %{
      member_level_name: d.member_level_name,
      member_month: d.member_month,
      user_comment: d.user_comment
    }

  defp details(_type, %{displayed_content: {:membership_gifting_details, d}}) when not is_nil(d),
    do: %{
      gift_memberships_count: d.gift_memberships_count,
      gift_memberships_level_name: d.gift_memberships_level_name
    }

  defp details(_type, %{displayed_content: {:gift_membership_received_details, d}})
       when not is_nil(d),
    do: %{
      member_level_name: d.member_level_name,
      gifter_channel_id: d.gifter_channel_id,
      associated_membership_gifting_message_id: d.associated_membership_gifting_message_id
    }

  defp details(_type, %{displayed_content: {:poll_details, d}}) when not is_nil(d),
    do: %{
      poll_status: poll_status(d.status),
      poll_question: d.metadata && d.metadata.question_text,
      poll_options: poll_options(d.metadata)
    }

  defp details(_type, %{displayed_content: {:gift_details, d}}) when not is_nil(d),
    do: %{
      gift_name: d.gift_name,
      jewels_amount: d.jewels_amount,
      combo_count: d.combo_count
    }

  defp details(_type, _snippet), do: %{}

  # --- shared helpers --------------------------------------------------------

  defp base_event(type, broadcaster_id, lane, message) do
    snippet = message.snippet

    %{
      type: type,
      platform: "youtube",
      lane: Atom.to_string(lane),
      broadcaster_user_id: broadcaster_id,
      msg_id: message.id,
      ts: snippet && snippet.published_at,
      received_at: DateTime.utc_now() |> DateTime.to_iso8601()
    }
  end

  defp put_flags(event, nil), do: event

  defp put_flags(event, author) do
    event
    |> Map.put(:is_chat_owner, author.is_chat_owner)
    |> Map.put(:is_chat_moderator, author.is_chat_moderator)
    |> Map.put(:is_chat_sponsor, author.is_chat_sponsor)
  end

  defp put_unless_nil(map, _key, nil), do: map
  defp put_unless_nil(map, key, value), do: Map.put(map, key, value)

  defp publish(publisher, subject, event) do
    case publisher.(subject, event) do
      :ok ->
        Metrics.count("Filter/Published")
        {:ok, subject}

      {:error, reason} ->
        Metrics.count("Filter/PublishErrors")
        {:error, reason}
    end
  end

  defp drop(reason) do
    Metrics.count("Filter/Dropped")
    Metrics.count("Filter/Dropped/" <> Atom.to_string(reason))
    {:dropped, reason}
  end

  defp lane_subject(lane), do: Config.lane_subject(lane)

  defp special_user?(author_id), do: MapSet.member?(Config.special_user_ids(), author_id)

  defp command?(text) when is_binary(text), do: String.starts_with?(text, @command_prefix)
  defp command?(_), do: false

  defp oversized?(text) when is_binary(text), do: byte_size(text) > Config.max_chat_text_bytes()
  defp oversized?(_), do: false

  # YouTube has no lowercase login handle; consumers keyed on
  # chatter_user_login get the lowercased display name.
  defp login(name) when is_binary(name), do: String.downcase(name)
  defp login(_), do: nil

  defp type_string(%LiveChatMessage{snippet: %{type: type}}) when is_atom(type),
    do: type |> Atom.to_string() |> String.downcase()

  defp type_string(_message), do: "invalid_type"

  defp poll_status(:active), do: "active"
  defp poll_status(:closed), do: "closed"
  defp poll_status(_), do: "unknown"

  defp poll_options(nil), do: []

  defp poll_options(metadata),
    do:
      Enum.map(metadata.options || [], fn option ->
        %{option_text: option.option_text, tally: option.tally}
      end)

  defp blank?(nil), do: true
  defp blank?(""), do: true
  defp blank?(_), do: false
end
