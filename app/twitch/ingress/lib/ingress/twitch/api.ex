# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Twitch.Api do
  require Logger

  alias Ingress.Config.Twitch, as: TwitchConfig
  alias Ingress.Twitch.AppToken

  @helix "https://api.twitch.tv/helix"

  @spec ensure_conduit() :: {:ok, String.t(), pos_integer()} | {:error, term()}
  def ensure_conduit do
    desired = TwitchConfig.conduit_shard_count()

    with {:ok, conduits} <- list_conduits() do
      case pick_conduit(conduits) do
        {:ok, %{"id" => id, "shard_count" => count}} when count < desired ->
          with :ok <- update_conduit(id, desired), do: {:ok, id, desired}

        {:ok, %{"id" => id, "shard_count" => count}} ->
          {:ok, id, count}

        {:error, reason} ->
          {:error, reason}
      end
    end
  end

  # Never substitute a conduit: outgress enrolls into the same TWITCH_CONDUIT_ID.
  defp pick_conduit(conduits) do
    case TwitchConfig.conduit_id() do
      nil ->
        {:error, :conduit_id_unset}

      "" ->
        {:error, :conduit_id_unset}

      id ->
        case Enum.find(conduits, &(&1["id"] == id)) do
          nil -> {:error, {:pinned_conduit_missing, id}}
          conduit -> {:ok, conduit}
        end
    end
  end

  def list_conduits do
    with {:ok, body} <- request(:get, "/eventsub/conduits", nil) do
      {:ok, body["data"] || []}
    end
  end

  def create_conduit(shard_count) do
    with {:ok, %{"data" => [%{"id" => id} | _]}} <-
           request(:post, "/eventsub/conduits", %{shard_count: shard_count}) do
      Logger.info("created conduit #{id} with #{shard_count} shards")
      {:ok, id}
    end
  end

  def update_conduit(conduit_id, shard_count) do
    with {:ok, _} <-
           request(:patch, "/eventsub/conduits", %{id: conduit_id, shard_count: shard_count}) do
      Logger.info("resized conduit #{conduit_id} to #{shard_count} shards")
      :ok
    end
  end

  @spec get_shards(String.t()) :: {:ok, [map()]} | {:error, term()}
  def get_shards(conduit_id), do: get_shards_page(conduit_id, nil, [])

  defp get_shards_page(conduit_id, cursor, acc) do
    path = "/eventsub/conduits/shards?conduit_id=" <> conduit_id <> cursor_param(cursor)

    with {:ok, body} <- request(:get, path, nil) do
      acc = [body["data"] || [] | acc]

      case get_in(body, ["pagination", "cursor"]) do
        cursor when cursor in [nil, ""] -> {:ok, acc |> Enum.reverse() |> Enum.concat()}
        next -> get_shards_page(conduit_id, next, acc)
      end
    end
  end

  defp cursor_param(nil), do: ""
  defp cursor_param(cursor), do: "&after=" <> URI.encode_www_form(cursor)

  @spec assign_shard(String.t(), non_neg_integer(), String.t()) :: :ok | {:error, term()}
  def assign_shard(conduit_id, shard_id, session_id) do
    payload = %{
      conduit_id: conduit_id,
      shards: [
        %{
          id: to_string(shard_id),
          transport: %{method: "websocket", session_id: session_id}
        }
      ]
    }

    case request(:patch, "/eventsub/conduits/shards", payload) do
      {:ok, %{"errors" => [_ | _] = errors}} -> {:error, {:shard_errors, errors}}
      {:ok, _body} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp request(method, path, json) do
    with {:ok, token} <- AppToken.get() do
      opts =
        [
          url: @helix <> path,
          method: method,
          headers: [
            {"client-id", TwitchConfig.client_id()},
            {"authorization", "Bearer " <> token}
          ]
        ] ++ if(json, do: [json: json], else: [])

      case Req.request(opts) do
        {:ok, %{status: status, body: body}} when status in 200..299 ->
          {:ok, body}

        {:ok, %{status: 401, body: body}} ->
          AppToken.invalidate()
          {:error, {:http, 401, body}}

        {:ok, %{status: status, body: body}} ->
          {:error, {:http, status, body}}

        {:error, reason} ->
          {:error, reason}
      end
    end
  end
end
