# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.Watcher do
  @moduledoc """
  Cluster-singleton discovery loop — the piece YouTube has no equivalent of
  Twitch's Conduit for. Twitch pushes every subscribed broadcaster's events
  into a pre-assigned shard fleet; YouTube requires us to notice, per channel,
  when a broadcast goes live, resolve its `activeLiveChatId`, and open a
  `ChatSession` for it.

  Every `YOUTUBE_WATCH_POLL_SECONDS` (default 30):

    * pinned live chat ids (dev escape hatch) get a session ensured directly;
    * each configured channel is asked over the Data API for its active
      broadcast (`liveBroadcasts.list`, 1 quota unit). Transitions:

        not live -> live    publish `stream.online`, start a session
        live -> live        same chat: no-op; new chat id: swap sessions
        live -> not live    stop the session, publish `stream.offline`

  Lifecycle events go to the stream lane and to the broadcaster's own lane,
  mirroring how the Twitch ingress dual-publishes stream.online/offline.
  Sessions are started through the Horde supervisor so placement follows
  `YtIngress.ChatDistribution`; a session that dies elsewhere in the cluster
  is restarted here on the next tick.

  The watch list itself comes from config today; the long-term source of truth
  is the users service over NATS RPC once its YouTube side ships.
  """

  use GenServer
  require Logger

  alias YtIngress.{BroadcasterCache, ChatSession, Config, Metrics, Nats, TokenSource}
  alias YtIngress.Config.YouTube, as: YouTubeConfig
  alias YtIngress.YouTube.Api

  defstruct channels: MapSet.new(),
            # channel_id => %{live_chat_id: String.t() | nil, video_id: String.t() | nil}
            live: %{},
            pinned: MapSet.new()

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: via())
  end

  def via, do: {:via, Horde.Registry, {YtIngress.Registry, :watcher}}

  @impl true
  def init(_opts) do
    state = %__MODULE__{
      channels: YouTubeConfig.channel_ids(),
      pinned: MapSet.new(YouTubeConfig.pinned_live_chat_ids())
    }

    schedule_tick()
    {:ok, state}
  end

  @impl true
  def handle_info(:tick, state) do
    state = ensure_pinned(state)
    state = reconcile_channels(state)
    schedule_tick()
    {:noreply, state}
  end

  # --- pinned chats (development) -------------------------------------------

  defp ensure_pinned(state) do
    Enum.each(state.pinned, fn live_chat_id ->
      key = {:pinned, live_chat_id}

      unless running?(key) do
        Logger.info("watcher: starting pinned chat session #{live_chat_id}")

        start_session(%{
          registry_key: key,
          channel_id: nil,
          live_chat_id: live_chat_id
        })
      end
    end)

    state
  end

  # --- channel reconciliation -------------------------------------------------

  defp reconcile_channels(state) do
    Enum.reduce(state.channels, state, fn channel_id, acc ->
      case discover(channel_id) do
        {:ok, %{live_chat_id: chat_id} = broadcast} when is_binary(chat_id) ->
          reconcile_live(acc, channel_id, broadcast)

        {:ok, :not_live} ->
          reconcile_offline(acc, channel_id)

        {:error, reason} ->
          Logger.warning(
            "watcher: discovery failed for #{channel_id}: #{inspect(reason)} (kept last state)"
          )

          Metrics.count("Watcher/DiscoveryErrors")
          acc
      end
    end)
  end

  defp reconcile_live(state, channel_id, broadcast) do
    previous = get_in(state.live, [channel_id, :live_chat_id])
    key = {:chat, channel_id}

    cond do
      previous == broadcast.live_chat_id and running?(key) ->
        state

      previous != nil and previous != broadcast.live_chat_id ->
        # A mid-stream restart produced a fresh chat id: swap sessions.
        stop_running(key)
        Metrics.count("Watcher/ChatSwapped")
        publish_lifecycle(channel_id, "stream.offline")
        start_and_record(state, channel_id, broadcast, "stream.online")

      true ->
        was_live = previous != nil

        state =
          start_and_record(state, channel_id, broadcast, if(was_live, do: nil, else: "stream.online"))

        unless was_live do
          Metrics.count("Watcher/StreamsOnline")
        end

        state
    end
  end

  defp reconcile_offline(state, channel_id) do
    case Map.get(state.live, channel_id) do
      %{live_chat_id: chat_id} when is_binary(chat_id) ->
        stop_running({:chat, channel_id})
        Metrics.count("Watcher/StreamsOffline")

        Logger.info("watcher: #{channel_id} went offline")
        publish_lifecycle(channel_id, "stream.offline")

        %{state | live: Map.put(state.live, channel_id, %{live_chat_id: nil, video_id: nil})}

      _ ->
        state
    end
  end

  defp start_and_record(state, channel_id, broadcast, lifecycle_event) do
    if lifecycle_event, do: publish_lifecycle(channel_id, lifecycle_event, broadcast)

    start_session(%{
      registry_key: {:chat, channel_id},
      channel_id: channel_id,
      live_chat_id: broadcast.live_chat_id
    })

    %{state | live: Map.put(state.live, channel_id, broadcast)}
  end

  defp discover(channel_id) do
    with {:ok, cred} <- TokenSource.authorize(channel_id) do
      Api.active_broadcast(channel_id, cred)
    end
  end

  # --- session plumbing -------------------------------------------------------

  defp running?(key) do
    case Horde.Registry.lookup(YtIngress.Registry, key) do
      [{pid, _}] -> alive?(pid)
      [] -> false
    end
  end

  # Process.alive?/1 is local-only: a remote session pid looks dead and the
  # watcher would start_child every tick. Same probe Bootstrapper uses.
  defp alive?(pid) when node(pid) == node(), do: Process.alive?(pid)

  defp alive?(pid) do
    case :rpc.call(node(pid), Process, :alive?, [pid], 5_000) do
      true -> true
      _ -> false
    end
  end

  defp stop_running(key) do
    case Horde.Registry.lookup(YtIngress.Registry, key) do
      [{pid, _}] ->
        GenServer.stop(pid, :normal)

      [] ->
        :ok
    end
  catch
    :exit, _ -> :ok
  end

  defp start_session(opts) do
    spec =
      {ChatSession,
       registry_key: opts.registry_key,
       channel_id: opts.channel_id,
       live_chat_id: opts.live_chat_id}

    case Horde.DynamicSupervisor.start_child(YtIngress.ChatSupervisor, spec) do
      {:ok, _pid} ->
        :ok

      {:error, {:already_started, _pid}} ->
        :ok

      {:error, reason} ->
        Logger.warning(
          "watcher: could not start session for #{inspect(opts[:registry_key])}: #{inspect(reason)}"
        )

        Metrics.count("Watcher/StartFailures")
    end
  end

  # `live_chat_id`/`video_id` ride every lifecycle event: outgress's chat
  # directory learns the send target from exactly these payloads (there is no
  # Data API discovery fallback on the send side — it would burn quota and
  # paper over a broken feed).
  defp publish_lifecycle(channel_id, type, broadcast \\ nil) do
    base = %{
      type: type,
      platform: "youtube",
      broadcaster_user_id: channel_id,
      live_chat_id: broadcast && broadcast.live_chat_id,
      video_id: broadcast && broadcast.video_id,
      msg_id: "#{type}:#{channel_id}:#{System.system_time(:millisecond)}",
      ts: DateTime.utc_now() |> DateTime.to_iso8601(),
      received_at: DateTime.utc_now() |> DateTime.to_iso8601()
    }

    # The stream lane always gets it; the broadcaster's own lane additionally
    # carries it so consumers can scope by broadcaster without subscribing to
    # the firehose-wide stream lane. Same dual-publish contract as Twitch.
    Nats.publish_event(Config.lane_subject(:stream), Map.put(base, :lane, "stream"))

    case BroadcasterCache.lane(channel_id) do
      :drop ->
        :ok

      lane ->
        Nats.publish_event(Config.lane_subject(lane), Map.put(base, :lane, Atom.to_string(lane)))
    end

    :ok
  end

  defp schedule_tick do
    Process.send_after(self(), :tick, YouTubeConfig.watcher_poll_seconds() * 1_000)
  end
end
