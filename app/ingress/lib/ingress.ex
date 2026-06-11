defmodule Ingress do
  @moduledoc """
  Twitch ingress: the Elixir/BEAM service that owns the Twitch EventSub
  Conduit and its WebSocket shards, filters incoming payloads, and forwards
  normalized events to NATS.

  See `Ingress.Application` for the supervision tree and
  `docs/src/content/docs/microservices/twitch-ingress.md` for the design.
  """
end
