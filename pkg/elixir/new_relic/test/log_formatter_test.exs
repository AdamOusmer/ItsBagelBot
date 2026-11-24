# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.LogFormatterTest do
  use ExUnit.Case, async: false

  alias Platform.LogFormatter

  setup do
    previous = System.get_env("NEW_RELIC_APP_NAME")

    on_exit(fn ->
      if previous,
        do: System.put_env("NEW_RELIC_APP_NAME", previous),
        else: System.delete_env("NEW_RELIC_APP_NAME")
    end)
  end

  for {label, app_name, entity} <- [
        {"tags the line with the New Relic entity", "ItsBagelBot-ControlsGateway",
         %{"entity.name" => "ItsBagelBot-ControlsGateway"}},
        {"leaves the entity off without an app name", nil, %{}}
      ] do
    test label do
      if unquote(app_name),
        do: System.put_env("NEW_RELIC_APP_NAME", unquote(app_name)),
        else: System.delete_env("NEW_RELIC_APP_NAME")

      line =
        LogFormatter.format(:info, ["TTS event ", "delivered"], {{2026, 11, 21}, {12, 0, 0, 0}},
          user_id: "42",
          event_id: "evt",
          trace_id: "5292b5d5721e379066e91a8897b7901a",
          reason: nil
        )
        |> IO.iodata_to_binary()

      assert String.ends_with?(line, "\n")

      assert JSON.decode!(line) ==
               Map.merge(
                 %{
                   "level" => "info",
                   "message" => "TTS event delivered",
                   "user_id" => "42",
                   "event_id" => "evt",
                   "trace.id" => "5292b5d5721e379066e91a8897b7901a"
                 },
                 unquote(Macro.escape(entity))
               )
    end
  end
end
