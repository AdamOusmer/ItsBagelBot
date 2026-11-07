# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AutomodTrialLaneTest do
  use Ingress.PublisherCase, async: false

  alias Ingress.AutomodTrialLane

  @subject "twitch.automod.trial.test"
  @meta %{
    shard_id: 0,
    msg_id: "m1",
    ts: "2026-06-10T00:00:00Z",
    origin: :trial,
    trial_generation: 8
  }

  defp chat(overrides \\ %{}) do
    event =
      Map.merge(
        %{
          "broadcaster_user_id" => "77",
          "chatter_user_id" => "1001",
          "message_id" => "chat-1",
          "message" => %{"text" => "hello"}
        },
        overrides
      )

    %{"subscription" => %{"type" => "channel.chat.message"}, "event" => event}
  end

  setup do
    start_fake_gnat(:gnat_bus)
    :ok
  end

  test "an unset subject does no work" do
    put_env(automod_trial_subject: nil)

    assert :ok == AutomodTrialLane.publish(chat(), @meta)
    refute_receive {:pub, _, _, _}, 100
  end

  test "a set subject reaches the bus connection" do
    put_env(automod_trial_subject: @subject)
    start_supervised!(AutomodTrialLane)

    assert :ok == AutomodTrialLane.publish(chat(), @meta)
    assert_receive {:pub, @subject, json, _opts}, 1_000

    assert {:ok, %{"kind" => "chat_input"}} =
             json |> IO.iodata_to_binary() |> Ingress.JSON.decode()
  end

  test "a missing lane process drops the copy and returns" do
    put_env(automod_trial_subject: @subject)

    assert :ok == AutomodTrialLane.publish(chat(), @meta)
    refute_receive {:pub, _, _, _}, 100
  end

  test "malformed payloads cost one copy, not the server" do
    put_env(automod_trial_subject: @subject)
    pid = start_supervised!(AutomodTrialLane)

    AutomodTrialLane.publish(chat(%{"broadcaster_user_id" => nil}), @meta)
    AutomodTrialLane.publish(chat(%{"message" => %{"text" => 42}}), @meta)
    AutomodTrialLane.publish(chat(), @meta)

    assert_receive {:pub, @subject, _json, _opts}, 1_000
    assert Process.whereis(AutomodTrialLane) == pid
  end
end
