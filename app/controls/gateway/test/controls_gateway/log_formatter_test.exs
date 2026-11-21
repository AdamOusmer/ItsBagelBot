# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.LogFormatterTest do
  use ExUnit.Case, async: true

  alias ControlsGateway.LogFormatter

  test "emits one JSON line that New Relic links to the trace" do
    line =
      LogFormatter.format(:info, ["TTS event ", "delivered"], {{2026, 11, 21}, {12, 0, 0, 0}},
        user_id: "42",
        event_id: "evt",
        trace_id: "5292b5d5721e379066e91a8897b7901a",
        reason: nil
      )
      |> IO.iodata_to_binary()

    assert String.ends_with?(line, "\n")

    assert Jason.decode!(line) == %{
             "level" => "info",
             "message" => "TTS event delivered",
             "user_id" => "42",
             "event_id" => "evt",
             "trace.id" => "5292b5d5721e379066e91a8897b7901a"
           }
  end
end
