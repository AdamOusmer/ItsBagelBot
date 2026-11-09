# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Nats.Server do
  @moduledoc "Consumer callback adapter for service-owned request handlers."
  defmacro __using__(_) do
    quote do
      use Gnat.Server
    end
  end
end
