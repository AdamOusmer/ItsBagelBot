defmodule Ingress.CacheInvalidator do
  @moduledoc """
  NATS consumer for broadcaster-status invalidation keys (subject from
  `NATS_CACHE_INVALIDATION_SUBJECT`).

  Accepted payloads:

    * `{"broadcaster_id": "12345"}` - evict one broadcaster
    * `{"all": true}` - flush the whole cache
    * a bare broadcaster ID as the message body
  """

  use Gnat.Server
  require Logger

  alias Ingress.BroadcasterCache

  @impl true
  def request(%{body: body}) do
    case Jason.decode(body) do
      {:ok, %{"all" => true}} ->
        Logger.info("cache invalidation: flush all")
        BroadcasterCache.invalidate_all()

      {:ok, %{"broadcaster_id" => id}} when is_binary(id) ->
        BroadcasterCache.invalidate(id)

      {:ok, id} when is_binary(id) ->
        BroadcasterCache.invalidate(id)

      _ ->
        case String.trim(body) do
          "" -> Logger.warning("unintelligible invalidation message: #{inspect(body)}")
          id -> BroadcasterCache.invalidate(id)
        end
    end

    :ok
  end

  @impl true
  def error(_message, error) do
    Logger.error("cache invalidator error: #{inspect(error)}")
    :ok
  end
end
