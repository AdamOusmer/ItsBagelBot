# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.LogFormatter do
  @moduledoc false

  @fields [
    device_id: "device_id",
    user_id: "user_id",
    event_id: "event_id",
    trace_id: "trace.id",
    reason: "reason"
  ]

  def format(level, message, _timestamp, metadata) do
    attributes =
      for {key, name} <- @fields, metadata[key] != nil, into: %{} do
        {name, to_string(metadata[key])}
      end

    line =
      Map.merge(attributes, %{
        "level" => Atom.to_string(level),
        "message" => IO.chardata_to_string(message)
      })

    [Jason.encode_to_iodata!(line), ?\n]
  end
end
