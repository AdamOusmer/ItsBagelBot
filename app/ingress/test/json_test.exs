# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary and unlicensed. See LICENSE.md.

defmodule Ingress.JSONTest do
  use ExUnit.Case, async: true

  alias Ingress.JSON

  defp encode(term), do: term |> JSON.encode() |> IO.iodata_to_binary()

  test "encodes date/time structs as ISO 8601 strings, not raw struct maps" do
    # Regression: native :json has no protocol dispatch, so a DateTime's tuple
    # :microsecond field raised {:unsupported_type, {_, 6}} and crashed the shard
    # publishing twitch.ingress.status.shard.up (since: DateTime.utc_now()).
    dt = ~U[2026-07-12 18:23:44.211650Z]
    assert encode(%{since: dt}) == ~s({"since":"2026-07-12T18:23:44.211650Z"})

    assert encode(%{at: ~N[2026-07-12 18:23:44.211650]}) ==
             ~s({"at":"2026-07-12T18:23:44.211650"})

    assert encode(%{d: ~D[2026-07-12]}) == ~s({"d":"2026-07-12"})
    assert encode(%{t: ~T[18:23:44.211650]}) == ~s({"t":"18:23:44.211650"})
  end

  test "still encodes the plain map/list/binary shapes the firehose uses" do
    assert encode(%{"a" => 1, "b" => [true, nil, "x"]}) in [
             ~s({"a":1,"b":[true,null,"x"]}),
             ~s({"b":[true,null,"x"],"a":1})
           ]
  end

  test "round-trips a status-shaped payload" do
    json = encode(%{shard_id: 1, node: "ingress@10.0.0.1", since: DateTime.utc_now()})
    assert {:ok, %{"shard_id" => 1, "since" => since}} = JSON.decode(json)
    assert {:ok, _, _} = DateTime.from_iso8601(since)
  end

  test "encodes atoms as strings, matching the Jason semantics the RPC replies had" do
    assert encode(%{reporter: :"ingress@10.0.0.1", state: :running, ok: true, gone: false}) =~
             ~s("reporter":"ingress@10.0.0.1")

    assert encode(%{ok: true, gone: false, missing: nil}) in [
             ~s({"gone":false,"missing":null,"ok":true}),
             ~s({"gone":false,"ok":true,"missing":null}),
             ~s({"missing":null,"gone":false,"ok":true}),
             ~s({"missing":null,"ok":true,"gone":false}),
             ~s({"ok":true,"gone":false,"missing":null}),
             ~s({"ok":true,"missing":null,"gone":false})
           ]
  end

  describe "members/1" do
    test "encodes the members without the enclosing braces" do
      assert IO.iodata_to_binary(JSON.members(%{a: 1})) == ~s("a":1)
    end

    test "closes into a valid object" do
      body = JSON.members(%{type: "stream.online", shard_id: 3, at: nil})

      assert {:ok, %{"type" => "stream.online", "shard_id" => 3, "at" => nil}} =
               JSON.decode(IO.iodata_to_binary([?{, body, ?}]))
    end

    test "escapes keys and values like a whole-map encode" do
      map = %{"a\"b" => "c\nd"}
      assert IO.iodata_to_binary([?{, JSON.members(map), ?}]) == encode(map)
    end
  end

  describe "Ingress.LaneMessage" do
    test "closes an object around shared members with its own lane" do
      body = JSON.members(%{type: "stream.online", shard_id: 3})

      assert encode(%Ingress.LaneMessage{lane: :stream, body: body}) ==
               ~s({"lane":"stream",) <> IO.iodata_to_binary(body) <> ~s(})
    end

    test "two lanes over one shared body decode to documents differing only in lane" do
      body = JSON.members(%{type: "stream.offline", shard_id: 3})

      assert {:ok, live} = JSON.decode(encode(%Ingress.LaneMessage{lane: :stream, body: body}))
      assert {:ok, own} = JSON.decode(encode(%Ingress.LaneMessage{lane: :premium, body: body}))

      assert live["lane"] == "stream"
      assert own["lane"] == "premium"
      assert Map.delete(live, "lane") == Map.delete(own, "lane")
    end
  end
end
