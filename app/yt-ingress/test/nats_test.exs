# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.NatsTest do
  use ExUnit.Case, async: true

  alias YtIngress.Nats

  describe "parse_pub_ack/1" do
    test "accepts a JetStream success ack" do
      assert :ok = Nats.parse_pub_ack(~s({"stream":"YOUTUBE_INGRESS","seq":42}))
    end

    test "rejects an error ack" do
      assert {:error, {:pub_ack, error}} =
               Nats.parse_pub_ack(
                 ~s({"stream":"YOUTUBE_INGRESS","seq":0,"error":{"code":503,"description":"no responders"}})
               )

      assert error["code"] == 503
    end

    test "treats a non-JetStream reply as unacked-but-accepted" do
      assert :ok = Nats.parse_pub_ack("{}")
    end
  end

  test "publish/2 degrades when the BUS connection is down" do
    # No supervision tree in unit tests: the connection name is unregistered.
    assert {:error, :not_connected} == Nats.publish("youtube.ingress.status.test", %{ok: true})
    assert {:error, :not_connected} ==
             Nats.publish_event("youtube.ingress.event.standard", %{ok: true})
  end

  test "publish/2 survives unencodable payloads without raising" do
    # A DateTime tuple field inside a non-DateTime struct position once
    # crashed the Twitch ingress publisher; the fire-and-forget path must
    # never take the caller down.
    poison = %{fn: fn -> :ok end}

    assert match?({:error, _}, Nats.publish("youtube.ingress.status.test", poison)) or
             :ok == Nats.publish("youtube.ingress.status.test", poison)
  end
end
