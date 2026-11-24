# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.LogFormatter do
  @moduledoc "One JSON line per log event for services without the agent log forwarder, tagged with the New Relic entity name. Metadata comes from the formatter's `metadata:` list; `trace_id` becomes `trace.id`."

  @renamed %{trace_id: "trace.id"}

  def format(level, message, _timestamp, metadata) do
    attributes =
      for {key, value} <- metadata, value != nil, into: %{} do
        {Map.get(@renamed, key, Atom.to_string(key)), to_string(value)}
      end

    line =
      attributes
      |> Map.merge(%{
        "level" => Atom.to_string(level),
        "message" => IO.chardata_to_string(message)
      })
      |> put_entity(System.get_env("NEW_RELIC_APP_NAME", ""))

    [JSON.encode_to_iodata!(line), ?\n]
  end

  defp put_entity(line, ""), do: line
  defp put_entity(line, name), do: Map.put(line, "entity.name", name)
end
