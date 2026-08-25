# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress do
  @moduledoc """
  YouTube ingress: the Elixir/BEAM service that owns the per-broadcaster
  `liveChatMessages.streamList` gRPC streams, normalizes incoming live chat
  events, and forwards them to NATS.

  See `YtIngress.Application` for the supervision tree and
  `docs/src/content/docs/microservices/youtube-ingress.md` for the design.
  """
end
