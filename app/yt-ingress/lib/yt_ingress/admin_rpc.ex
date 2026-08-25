# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.AdminRpc do
  @moduledoc """
  Request-reply endpoint for the admin tool: a cluster-wide snapshot of the
  watcher's view and every chat session, gathered through the Horde registry
  wherever each session currently runs. A queue group ensures exactly one
  replica answers.

      request:  {}
      reply:    {"watched_channels": N, "sessions": [%{
                  "channel_id": "...", "live_chat_id": "...",
                  "connected": true, "attempts": 0, "resumed": false,
                  "node": "yt-ingress-a@10.42.0.7"}], ...}
  """

  use Gnat.Server
  require Logger

  alias YtIngress.{ChatSession, JSON}
  alias YtIngress.Config.YouTube, as: YouTubeConfig

  @per_session_timeout_ms 2_000

  @impl true
  def request(%{body: _body}) do
    channel_sessions =
      Horde.Registry.select(YtIngress.Registry, [
        {{{:chat, :"$1"}, :"$2", :_}, [], [{{:"$1", :"$2"}}]}
      ])
      |> Enum.map(fn {channel_id, pid} -> session_entry(channel_id, pid) end)

    # Pinned dev chats have no channel id; report them by chat id.
    sessions = Enum.concat(channel_sessions, pinned_entries())

    {:reply,
     JSON.encode(%{
       watched_channels: MapSet.size(YouTubeConfig.channel_ids()),
       pinned_chats: MapSet.size(YouTubeConfig.pinned_live_chat_ids()),
       sessions: sessions
     })}
  rescue
    error ->
      Logger.error("admin snapshot failed: #{inspect(error)}")
      {:reply, JSON.encode(%{"error" => inspect(error)})}
  end

  @impl true
  def error(_message, error) do
    Logger.error("admin rpc error: #{inspect(error)}")
    :ok
  end

  defp session_entry(channel_id, pid) do
    case ChatSession.status(pid, @per_session_timeout_ms) do
      %{channel_id: channel_id} = status ->
        %{
          channel_id: channel_id,
          live_chat_id: status.live_chat_id,
          connected: status.connected?,
          attempts: status.attempts,
          resumed: status.resumed?,
          node: to_string(status.node)
        }

      other ->
        other
    end
  catch
    :exit, reason -> %{channel_id: channel_id, error: inspect(reason)}
  end

  defp pinned_entries do
    Enum.flat_map(YouTubeConfig.pinned_live_chat_ids(), fn live_chat_id ->
      case Horde.Registry.lookup(YtIngress.Registry, {:pinned, live_chat_id}) do
        [{pid, _}] ->
          [
            case ChatSession.status(pid, @per_session_timeout_ms) do
              %{live_chat_id: id} = status ->
                %{live_chat_id: id, connected: status.connected?, node: to_string(status.node)}

              _ ->
                %{live_chat_id: live_chat_id, connected: false}
            end
          ]

        [] ->
          []
      end
    end)
  end
end
