# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Cowboy.Handler do
  @moduledoc "Application callbacks for a server-side WebSocket."
  @type frame :: {:text | :binary | :ping | :pong, binary()}
  @type frames :: frame() | [frame()]
  @type result ::
          {:ok, term()}
          | {:push, frames(), term()}
          | {:stop, integer(), binary(), term()}
          | {:ok, term(), :hibernate}
          | {:push, frames(), term(), :hibernate}
  @callback init(term()) :: result()
  @callback handle_in({:text | :binary, binary()}, term()) :: result()
  @callback handle_info(term(), term()) :: result()
  @callback terminate(term(), term()) :: term()
end
