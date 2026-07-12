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
end
