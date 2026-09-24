# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.LaneMessage do
  @enforce_keys [:lane, :body]
  defstruct [:lane, :body]

  @type t :: %__MODULE__{lane: atom(), body: iodata()}
end
