# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Sift.DecodeError do
  defexception [:reason]

  @impl true
  def message(%{reason: reason}) when is_binary(reason), do: "invalid JSON: #{reason}"
  def message(%{reason: reason}), do: "invalid JSON: #{inspect(reason)}"
end

defmodule Platform.Sift.EncodeError do
  defexception [:reason]

  @impl true
  def message(%{reason: reason}), do: "cannot encode as JSON: #{inspect(reason)}"
end
