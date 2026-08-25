# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.PipelineTest do
  use ExUnit.Case, async: true

  alias Youtube.Api.V3.{
    LiveChatMessage,
    LiveChatMessageAuthorDetails,
    LiveChatMessageSnippet,
    LiveChatSuperChatDetails
  }

  alias YtIngress.Pipeline

  @broadcaster "UCbroadcaster"

  # Injectable deps: no NATS, no broadcaster cache — the routing logic is the
  # unit under test.
  defp deps(lane \\ :standard) do
    [
      lane_lookup: fn _broadcaster -> lane end,
      publisher: fn subject, event ->
        send(self(), {:published, subject, event})
        :ok
      end
    ]
  end

  defp text_message(text, opts \\ []) do
    author_id = Keyword.get(opts, :author, "UCviewer")
    display_name = Keyword.get(opts, :display_name, "Viewer")

    %LiveChatMessage{
      id: "msg-1",
      snippet: %LiveChatMessageSnippet{
        type: :TEXT_MESSAGE_EVENT,
        published_at: "2026-08-22T00:00:00Z",
        display_message: text,
        author_channel_id: author_id
      },
      author_details: %LiveChatMessageAuthorDetails{
        channel_id: author_id,
        display_name: display_name
      }
    }
  end

  defp super_chat(micros \\ 1_750_000) do
    %LiveChatMessage{
      id: "sc-1",
      snippet: %LiveChatMessageSnippet{
        type: :SUPER_CHAT_EVENT,
        published_at: "2026-08-22T00:00:01Z",
        # protobuf-elixir oneof shape: {field_name, value}
        displayed_content:
          {:super_chat_details,
           %LiveChatSuperChatDetails{
             amount_micros: micros,
             currency: "USD",
             amount_display_string: "$1.75",
             tier: 1
           }},
        author_channel_id: "UCsupporter"
      },
      author_details: %LiveChatMessageAuthorDetails{channel_id: "UCsupporter"}
    }
  end

  defp tombstone do
    %LiveChatMessage{id: "tomb-1", snippet: %LiveChatMessageSnippet{type: :TOMBSTONE}}
  end

  test "plain chatter is dropped — only commands and special authors cross the bus" do
    assert {:dropped, :plain_chat} =
             Pipeline.handle_message(text_message("hello world"), @broadcaster, deps())
  end

  test "command text routes to the broadcaster's lane" do
    assert {:ok, subject} = Pipeline.handle_message(text_message("!points"), @broadcaster, deps())
    assert subject == "youtube.ingress.event.standard"
  end

  test "special authors always ride the premium lane" do
    Application.put_env(:yt_ingress, :special_user_ids, MapSet.new(["UCspecial"]))

    assert {:ok, subject} =
             Pipeline.handle_message(text_message("hello", author: "UCspecial"), @broadcaster, deps())

    assert subject == "youtube.ingress.event.premium"
  after
    Application.put_env(:yt_ingress, :special_user_ids, MapSet.new())
  end

  test "premium broadcasters route command traffic to the premium lane" do
    assert {:ok, "youtube.ingress.event.premium"} =
             Pipeline.handle_message(text_message("!so"), @broadcaster, deps(:premium))
  end

  test "a banned broadcaster's commands are dropped" do
    assert {:dropped, :broadcaster_banned} =
             Pipeline.handle_message(text_message("!so"), @broadcaster, deps(:drop))
  end

  test "chat payload mirrors the Twitch wire shape plus platform extras" do
    {:ok, subject} = Pipeline.handle_message(text_message("!so"), @broadcaster, deps())

    assert_received({:published, ^subject, event})

    assert event.type == "text_message_event"
    assert event.platform == "youtube"
    assert event.lane == "standard"
    assert event.broadcaster_user_id == @broadcaster
    assert event.chatter_user_id == "UCviewer"
    assert event.chatter_user_login == "viewer"
    assert event.chatter_user_name == "Viewer"
    assert event.text == "!so"
    assert event.ts == "2026-08-22T00:00:00Z"
    assert event.msg_id == "msg-1"
    assert is_binary(event.received_at)
  end

  test "super chat publishes to the broadcaster's lane with flattened details" do
    assert {:ok, subject} = Pipeline.handle_message(super_chat(), @broadcaster, deps())
    assert subject == "youtube.ingress.event.standard"

    assert_received({:published, ^subject, event})
    assert event.type == "super_chat_event"
    assert event.amount_micros == 1_750_000
    assert event.currency == "USD"
    assert event.tier == 1
    assert event.msg_id == "sc-1"
  end

  test "non-actionable types are dropped" do
    assert {:dropped, :not_actionable} =
             Pipeline.handle_message(tombstone(), @broadcaster, deps())
  end

  test "oversized command text is dropped" do
    Application.put_env(:yt_ingress, :max_chat_text_bytes, 16)

    assert {:dropped, :oversized} =
             Pipeline.handle_message(
               text_message(String.duplicate("!", 32)),
               @broadcaster,
               deps()
             )
  after
    Application.put_env(:yt_ingress, :max_chat_text_bytes, 4_096)
  end
end
