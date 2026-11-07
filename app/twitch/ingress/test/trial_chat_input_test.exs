# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialChatInputTest do
  use ExUnit.Case, async: true

  import Ingress.EnvCase

  alias Ingress.Pipeline

  @special MapSet.new(["1001", "1002"])
  @meta %{shard_id: 0, msg_id: "m1", ts: "2026-06-10T00:00:00Z"}

  defp notification(type, event) do
    %{"subscription" => %{"type" => type}, "event" => event}
  end

  test "the automod trial lane carries one validated chat_input, unfolded and trial-marked" do
    event = %{
      "broadcaster_user_id" => "77",
      "broadcaster_user_login" => "room",
      "chatter_user_id" => "1001",
      "chatter_user_login" => "viewer",
      "message_id" => "chat-1",
      "message_type" => "user_intro",
      "badges" => [%{"set_id" => "vip", "id" => "1", "info" => ""}],
      "message" => %{"text" => "hello <chat>"}
    }

    put_env(special_user_ids: @special)
    meta = Map.merge(@meta, %{origin: :trial, trial_generation: 8})
    ingested_at = ~U[2026-11-04 12:00:01Z]

    input =
      Pipeline.trial_chat_input(notification("channel.chat.message", event), meta, ingested_at)

    assert %{
             kind: "chat_input",
             schema_version: 1,
             routing_version: 2,
             envelope: %{
               lane: :standard,
               origin: "trial",
               trial_generation: 8,
               first_message: true,
               text: "hello <chat>"
             }
           } = input

    assert input.partition ==
             :crypto.hash(:sha256, "77")
             |> binary_part(0, 8)
             |> :binary.decode_unsigned()
             |> rem(128)

    refute Map.has_key?(input.envelope, :shard_id)

    assert %{"envelope" => %{"lane" => "standard"}} =
             input |> Ingress.JSON.encode() |> IO.iodata_to_binary() |> :json.decode()

    assert nil ==
             Pipeline.trial_chat_input(notification("channel.follow", event), meta, ingested_at)

    if path = System.get_env("AUTOMOD_TRIAL_FIXTURE_OUT") do
      File.write!(path, Ingress.JSON.encode(input))
    end
  end
end
