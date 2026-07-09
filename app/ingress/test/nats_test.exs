defmodule Ingress.NatsTest do
  use ExUnit.Case, async: true

  alias Ingress.Nats

  describe "parse_pub_ack/1" do
    test "a stored message acks" do
      assert Nats.parse_pub_ack(~s({"stream":"TWITCH_INGRESS","seq":42})) == :ok
    end

    test "a duplicate inside the dedup window is success (already stored once)" do
      assert Nats.parse_pub_ack(~s({"stream":"TWITCH_INGRESS","seq":42,"duplicate":true})) == :ok
    end

    test "a JetStream error refuses the publish" do
      body = ~s({"error":{"code":503,"description":"no responders"}})
      assert {:error, {:pub_ack, %{"code" => 503}}} = Nats.parse_pub_ack(body)
    end

    test "an unintelligible ack is an error, not a silent success" do
      assert Nats.parse_pub_ack("not json") == {:error, :bad_pub_ack}
      assert Nats.parse_pub_ack(~s({"unexpected":true})) == {:error, :bad_pub_ack}
    end
  end
end
