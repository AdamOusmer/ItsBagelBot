# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.CacheInvalidator do
  use Ingress.RpcServer, log: "cache invalidator"

  alias Ingress.{BroadcasterCache, JSON}

  @impl true
  def request(%{body: body}) do
    case JSON.decode(body) do
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
end
