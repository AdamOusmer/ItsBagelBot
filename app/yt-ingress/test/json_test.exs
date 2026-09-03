# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.JSONTest do
  use ExUnit.Case, async: true

  alias YtIngress.JSON

  test "encodes maps with nils as nulls" do
    {:ok, decoded} = JSON.encode(%{a: 1, b: nil}) |> IO.iodata_to_binary() |> JSON.decode()

    assert decoded == %{"a" => 1, "b" => nil}
  end

  test "encodes DateTime structs as ISO 8601 strings" do
    encoded =
      %{
        since:
          DateTime.from_iso8601("2026-08-22T00:00:00Z") |> elem(1)
      }
      |> JSON.encode()
      |> IO.iodata_to_binary()

    assert encoded == ~s({"since":"2026-08-22T00:00:00Z"})
  end

  test "round-trips a lane event" do
    event = %{type: "text_message_event", platform: "youtube", msg_id: "abc"}

    {:ok, decoded} = event |> JSON.encode() |> IO.iodata_to_binary() |> JSON.decode()

    assert decoded == %{
             "type" => "text_message_event",
             "platform" => "youtube",
             "msg_id" => "abc"
           }
  end

  test "decode reports trailing data" do
    assert {:error, {:trailing_data, " junk"}} = JSON.decode(~s({"a":1} junk))
  end
end
