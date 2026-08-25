# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.YouTube.Api do
  @moduledoc """
  YouTube Data API v3 REST calls the ingress needs beyond the streamList gRPC
  surface: finding each watched channel's currently-active broadcast and its
  live chat.

  Discovery polls `liveBroadcasts.list` with `mine=true`, which costs 1 quota
  unit per call — that is why the watcher's cadence is a config value (see
  `YtIngress.Config.YouTube.watcher_poll_seconds/0`). The caller supplies the
  credential from `YtIngress.TokenSource`: OAuth bearer for production, API
  key for development.
  """


  alias YtIngress.Config.YouTube, as: YouTubeConfig
  alias YtIngress.TokenSource

  @doc """
  Returns `{:ok, %{video_id:, live_chat_id:, title:}}` for the channel's live
  broadcast, `{:ok, :not_live}` when none is active, or `{:error, reason}`.
  """
  @spec active_broadcast(String.t(), TokenSource.cred()) ::
          {:ok, %{video_id: String.t(), live_chat_id: String.t(), title: String.t() | nil}}
          | {:ok, :not_live}
          | {:error, term()}
  def active_broadcast(_channel_id, cred) do
    request =
      Req.new(
        base_url: YouTubeConfig.data_api_url(),
        retry: false,
        connect_options: [timeout: 5_000]
      )

    request =
      case cred do
        {:bearer, token} -> Req.Request.put_header(request, "authorization", "Bearer " <> token)
        # The API key rides the query string instead of a header.
        {:api_key, _key} -> request
      end

    params =
      [
        part: "id,snippet,status",
        mine: "true",
        broadcastType: "all",
        maxResults: 10
      ] |> maybe_api_key(cred)

    case Req.get(request, url: "/liveBroadcasts", params: params) do
      {:ok, %Req.Response{status: 200, body: body}} ->
        broadcast =
          Enum.find(body["items"] || [], fn item ->
            get_in(item, ["status", "lifeCycleStatus"]) == "live"
          end)

        case broadcast do
          nil ->
            {:ok, :not_live}

          %{"id" => video_id} = item ->
            {:ok,
             %{
               video_id: video_id,
               live_chat_id: get_in(item, ["snippet", "liveChatId"]),
               title: get_in(item, ["snippet", "title"])
             }}
        end

      {:ok, %Req.Response{status: status, body: body}} ->
        {:error, {:api_status, status, api_error_message(body)}}

      {:error, exception} ->
        {:error, exception}
    end
  rescue
    error -> {:error, error}
  catch
    :exit, reason -> {:error, {:exit, reason}}
  end

  defp maybe_api_key(params, {:api_key, key}), do: Keyword.put(params, :key, key)
  defp maybe_api_key(params, _), do: params

  defp api_error_message(%{"error" => %{"message" => message}}), do: message
  defp api_error_message(_body), do: nil
end
