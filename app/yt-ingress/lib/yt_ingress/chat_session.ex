# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.ChatSession do
  @moduledoc """
  Owns one `liveChatMessages.streamList` gRPC server-stream: one GenServer per
  active live chat, registered cluster-wide in `YtIngress.Registry` under
  `{:chat, channel_id}` (or `{:pinned, live_chat_id}` for dev-pinned chats),
  supervised by `YtIngress.ChatSupervisor` (Horde) so ownership moves to a
  surviving node when this one dies.

  Protocol obligations this process encodes:

    * **Resume, don't replay**: every response carries a `next_page_token`.
      Reconnects pass the last token back as `page_token`, so YouTube resumes
      exactly where we left off instead of replaying history. Only a cold
      start (no token yet) receives the bounded recent-history window;
      consumers de-duplicate on `msg_id` regardless.

    * **End detection**: a response carrying `offline_at` means the underlying
      livestream ended; the session exits cleanly (`{:shutdown, :ended}`) and
      lets the watcher reconcile. The same applies to `NOT_FOUND` and
      `FAILED_PRECONDITION` (live chat disabled / ended), which are terminal
      gRPC statuses for this RPC.

    * **No zombie streams**: an idle watchdog reconnects if nothing arrives
      within `YT_CHAT_IDLE_TIMEOUT_SECONDS` (default 300). A silent HTTP/2
      stream through proxies is indistinguishable from a dead one; resuming is
      cheap, so prefer proactive reconnects over trust.

    * **Auth healing**: `PERMISSION_DENIED`/`UNAUTHENTICATED` triggers exactly
      one forced token refetch before giving up, so a rotated access token
      heals mid-stream without a restart.

  Restart strategy is `:transient`: normal end/shutdown does not restart, a
  crash does, with jittered exponential backoff capped at 60 seconds.
  """

  use GenServer, restart: :transient
  require Logger

  alias GRPC.{Credential, RPCError, Stub}
  alias Youtube.Api.V3.LiveChatMessageListRequest
  alias Youtube.Api.V3.V3DataLiveChatMessageService, as: ChatService
  alias YtIngress.{Metrics, Nats, Pipeline, TokenSource}
  alias YtIngress.Config.YouTube, as: YouTubeConfig

  # Jittered exponential backoff, identical shape to the Twitch ingress.
  @base_backoff_ms 1_000
  @max_backoff_ms 60_000

  # How often a session verifies it still holds its cluster-wide name. A
  # registry CRDT merge after a netsplit heal can drop registrations for
  # processes that are still alive (observed on the Twitch ingress on
  # 2026-08-20): an unregistered-but-streaming session is invisible to the
  # watcher, admin snapshot and drain, so re-register proactively.
  @registry_check_interval_ms 30_000

  defstruct [
    :channel_id,
    :live_chat_id,
    :grpc_channel,
    :reader_ref,
    :page_token,
    :backoff_timer,
    :watchdog_timer,
    :registry_check_timer,
    attempts: 0,
    connected?: false,
    auth_retries: 0,
    # Whether this session believes it holds its cluster-wide name:
    # :named normally, :released after handing the name to a drain successor.
    name_state: :named
  ]

  def start_link(opts) do
    key = Keyword.fetch!(opts, :registry_key)
    GenServer.start_link(__MODULE__, opts, name: via(key))
  end

  def via(key), do: {:via, Horde.Registry, {YtIngress.Registry, key}}

  @doc """
  Snapshot of the session's live state, served from wherever in the cluster
  the session currently runs. Used by `YtIngress.AdminRpc` and `YtIngress.Drain`.
  """
  def status(pid, timeout \\ 2_000), do: GenServer.call(pid, :status, timeout)

  @impl true
  def init(opts) do
    state = %__MODULE__{
      channel_id: Keyword.get(opts, :channel_id),
      live_chat_id: Keyword.fetch!(opts, :live_chat_id),
      page_token: Keyword.get(opts, :page_token)
    }

    registry_check_timer =
      Process.send_after(self(), :check_registry_name, @registry_check_interval_ms)

    send(self(), :connect)

    {:ok, %{state | registry_check_timer: registry_check_timer}}
  end

  @impl true
  def handle_call(:status, _from, state) do
    {:reply,
     %{
       channel_id: state.channel_id,
       live_chat_id: state.live_chat_id,
       connected?: state.connected?,
       attempts: state.attempts,
       resumed?: state.page_token != nil,
       node: node()
     }, state}
  end

  # Drain handoff: free the cluster-wide name so the successor can take it,
  # while this process keeps streaming until it is stopped.
  def handle_call(:release_name, _from, state) do
    Horde.Registry.unregister(YtIngress.Registry, registry_key(state))
    {:reply, :ok, %{state | name_state: :released}}
  end

  @impl true
  def handle_info(:connect, state) do
    state = cancel_watchdog(state)
    Metrics.count("Session/ConnectAttempts")

    case TokenSource.authorize(state.channel_id) do
      {:ok, cred} ->
        open_stream(state, cred)

      {:error, reason} ->
        Logger.warning(
          "chat #{state.live_chat_id}: no credential (#{inspect(reason)}); retrying with backoff"
        )

        Metrics.count("Session/CredentialFailures")
        schedule_reconnect(state)
    end
  end

  def handle_info({:chat_progress, _response_meta}, state) do
    # Reader heartbeat: responses keep arriving even when they carry no items.
    {:noreply, arm_watchdog(state)}
  end

  def handle_info({:page_token, token}, state) when is_binary(token) do
    unless state.connected? do
      announce_up(state)
    end

    {:noreply,
     %{
       state
       | page_token: token,
         connected?: true,
         attempts: 0,
         auth_retries: 0
     }}
  end

  def handle_info({:offline_at, _ts}, state) do
    Logger.info("chat #{state.live_chat_id}: livestream ended (offline_at received)")
    Metrics.count("Session/Ended")
    announce_down(state, "ended")
    {:stop, {:shutdown, :ended}, state}
  end

  def handle_info({:rpc_error, %RPCError{} = error}, state) do
    case classify(error) do
      :ended ->
        Logger.info("chat #{state.live_chat_id}: stream over (#{error.message})")
        Metrics.count("Session/Ended")
        announce_down(state, "ended")
        {:stop, {:shutdown, :ended}, state}

      :auth when state.auth_retries < 1 and is_binary(state.channel_id) ->
        Logger.warning("chat #{state.live_chat_id}: auth failure; forcing token refresh")
        Metrics.count("Session/AuthRefresh")
        TokenSource.refresh(state.channel_id)
        {:noreply, reconnect_now(%{state | auth_retries: state.auth_retries + 1})}

      :auth ->
        Logger.error("chat #{state.live_chat_id}: auth failure persists; stopping for backoff")
        Metrics.count("Session/AuthCrashes")
        announce_down(state, "unauthorized")
        {:stop, {:shutdown, :unauthorized}, state}

      :rate ->
        Logger.warning("chat #{state.live_chat_id}: rate limited by YouTube; backing off")
        Metrics.count("Session/RateLimited")
        schedule_reconnect(%{state | attempts: max(state.attempts, 3)})

      :transient ->
        Logger.warning(
          "chat #{state.live_chat_id}: stream error #{GRPC.Status.code_name(error.status)} " <>
            "(#{error.message}); reconnecting"
        )

        Metrics.count("Session/TransientErrors")
        schedule_reconnect(%{state | attempts: state.attempts + 1})
    end
  end

  def handle_info({:stream_closed, :normal}, state) do
    # Server closed cleanly without offline_at: treat as ended; the watcher's
    # next reconcile decides whether a new broadcast took its place.
    Logger.info("chat #{state.live_chat_id}: stream closed by server")
    Metrics.count("Session/ClosedByServer")
    announce_down(state, "closed")
    {:stop, {:shutdown, :ended}, state}
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, state)
      when ref == state.reader_ref and reason != :normal do
    Logger.warning("chat #{state.live_chat_id}: reader crashed: #{inspect(reason)}")
    Metrics.count("Session/ReaderCrashes")
    schedule_reconnect(%{state | attempts: state.attempts + 1})
  end

  def handle_info({:DOWN, _ref, :process, _pid, _reason}, state) do
    {:noreply, %{state | reader_ref: nil}}
  end

  def handle_info(:idle_timeout, state) do
    Logger.warning("chat #{state.live_chat_id}: idle watchdog fired; reconnecting")
    Metrics.count("Session/IdleTimeouts")
    {:noreply, reconnect_now(%{state | attempts: state.attempts + 1})}
  end

  def handle_info(:check_registry_name, state) do
    timer = Process.send_after(self(), :check_registry_name, @registry_check_interval_ms)
    state = %{state | registry_check_timer: timer}

    if state.name_state == :named do
      key = registry_key(state)

      unless registered?(key, self()) do
        Logger.warning("chat #{state.live_chat_id}: lost registry name; re-registering")
        Metrics.count("Session/RegistryRepair")
        Horde.Registry.register(YtIngress.Registry, key, nil)
      end
    end

    {:noreply, state}
  end

  @impl true
  def terminate(_reason, state) do
    if state.watchdog_timer, do: Process.cancel_timer(state.watchdog_timer)
    if state.backoff_timer, do: Process.cancel_timer(state.backoff_timer)
    if state.registry_check_timer, do: Process.cancel_timer(state.registry_check_timer)

    safe_cancel(state.grpc_channel)

    :ok
  end

  # --- connection ------------------------------------------------------------

  defp open_stream(state, cred) do
    with {:ok, channel} <- connect_channel(),
         {:ok, _session_channel, reader} <- start_reader(state, channel, cred) do
      Logger.info(
        "chat #{state.live_chat_id}: streamList open on #{node()} " <>
          "(resumed: #{state.page_token != nil})"
      )

      watchdog = Process.send_after(self(), :idle_timeout, YouTubeConfig.chat_idle_timeout_seconds() * 1_000)

      {:noreply,
       %{
         state
         | grpc_channel: channel,
           reader_ref: reader.ref,
           watchdog_timer: watchdog,
           connected?: false
       }}
    else
      {:error, reason} ->
        Logger.warning("chat #{state.live_chat_id}: connect failed: #{inspect(reason)}")
        Metrics.count("Session/ConnectFailures")
        schedule_reconnect(%{state | attempts: state.attempts + 1})
    end
  end

  defp connect_channel do
    target = "#{YouTubeConfig.grpc_host()}:#{YouTubeConfig.grpc_port()}"

    tls =
      Credential.new(ssl: [
        verify: :verify_peer,
        cacertfile: CAStore.file_path(),
        depth: 3
      ])

    Stub.connect(target, cred: tls, connect_timeout: 10_000)
  rescue
    error -> {:error, error}
  catch
    :exit, reason -> {:error, {:connect_exit, reason}}
  end

  # The reader owns response enumeration AND the hot path (normalization +
  # publish): blocking it applies TCP backpressure naturally, while the
  # session process stays free for lifecycle control. The generated stub for
  # a server-streaming RPC returns `{:ok, enumerable}` whose elements are
  # `{:ok, response}` / `{:error, %GRPC.RPCError{}}`.
  defp start_reader(state, channel, cred) do
    request = %LiveChatMessageListRequest{
      live_chat_id: state.live_chat_id,
      part: ["id", "snippet", "authorDetails"],
      page_token: state.page_token
    }

    metadata = TokenSource.grpc_metadata(cred)
    session = self()
    broadcaster_id = state.channel_id

    task =
      Task.Supervisor.async_nolink(__MODULE__.TaskSupervisor, fn ->
        case channel |> ChatService.Stub.stream_list(request, metadata: metadata) do
          {:ok, responses} ->
            Enum.reduce_while(responses, :open, fn
              {:ok, response} ->
                send(session, {:page_token, response.next_page_token})

                Enum.each(response.items || [], fn item ->
                  Pipeline.handle_message(item, broadcaster_id)
                end)

                if response.offline_at do
                  send(session, {:offline_at, response.offline_at})
                  {:halt, :ended}
                else
                  send(session, {:chat_progress, nil})
                  {:cont, :open}
                end

              {:error, %RPCError{} = error} ->
                send(session, {:rpc_error, error})
                {:halt, :error}

              {:error, other} ->
                send(
                  session,
                  {:rpc_error,
                   RPCError.exception(status: GRPC.Status.unknown(), message: inspect(other))}
                )

                {:halt, :error}
            end)
            |> case do
              :open -> send(session, {:stream_closed, :normal})
              _ -> :ok
            end

          {:error, %RPCError{} = error} ->
            send(session, {:rpc_error, error})
        end
      end)

    {:ok, channel, task}
  rescue
    error -> {:error, error}
  end

  # Teardown closes this session's whole gRPC channel (one channel per
  # session), which tears down its HTTP/2 streams and makes the reader fail
  # fast. The stream handle itself lives inside the reader, so killing the
  # connection process is the reliable cancel path.
  defp safe_cancel(nil), do: :ok

  defp safe_cancel(%GRPC.Channel{adapter_payload: adapter_payload}) when is_map(adapter_payload) do
    case adapter_payload do
      %{conn_pid: pid} when is_pid(pid) -> Process.exit(pid, :kill)
      _ -> :ok
    end
  rescue
    _ -> :ok
  catch
    :exit, _ -> :ok
  end

  defp safe_cancel(_other), do: :ok

  # --- lifecycle helpers -----------------------------------------------------

  # Returns a handle_info reply. Callers used to return this map bare, which
  # is a GenServer crash (`:bad_return_value`) — a :transient restart that
  # drops the page_token and turns a backoff into a reconnect storm.
  defp schedule_reconnect(state) do
    state = close_transport(state)
    delay = backoff_ms(state.attempts)
    Metrics.count("Session/Reconnects")

    timer = Process.send_after(self(), :connect, delay)
    {:noreply, %{state | backoff_timer: timer, grpc_channel: nil, reader_ref: nil, connected?: false}}
  end

  defp reconnect_now(state) do
    state = close_transport(state)
    send(self(), :connect)
    %{state | grpc_channel: nil, reader_ref: nil, connected?: false}
  end

  defp close_transport(state) do
    state = cancel_watchdog(state)

    if state.reader_ref, do: Process.demonitor(state.reader_ref, [:flush])
    safe_cancel(state.grpc_channel)

    state
  end

  defp backoff_ms(attempts) when attempts <= 0, do: @base_backoff_ms

  defp backoff_ms(attempts) do
    min(@base_backoff_ms * Integer.pow(2, min(attempts - 1, 6)), @max_backoff_ms) +
      :rand.uniform(1_000)
  end

  defp arm_watchdog(state) do
    state = cancel_watchdog(state)
    %{state | watchdog_timer: Process.send_after(self(), :idle_timeout, YouTubeConfig.chat_idle_timeout_seconds() * 1_000)}
  end

  defp cancel_watchdog(%{watchdog_timer: nil} = state), do: state

  defp cancel_watchdog(state) do
    Process.cancel_timer(state.watchdog_timer)
    %{state | watchdog_timer: nil}
  end

  # --- status ----------------------------------------------------------------

  defp announce_up(state) do
    Nats.publish(status_subject("up"), %{
      channel_id: state.channel_id,
      live_chat_id: state.live_chat_id,
      node: node(),
      since: DateTime.utc_now()
    })

    Metrics.event("chat.up", %{channel_id: state.channel_id})
  end

  defp announce_down(state, reason) do
    Nats.publish(status_subject("down"), %{
      channel_id: state.channel_id,
      live_chat_id: state.live_chat_id,
      node: node(),
      reason: reason
    })

    Metrics.event("chat.down", %{channel_id: state.channel_id, reason: reason})
  end

  defp status_subject(direction), do: "youtube.ingress.status.chat." <> direction

  # --- misc ------------------------------------------------------------------

  defp classify(%RPCError{status: status}) do
    cond do
      status == GRPC.Status.not_found() -> :ended
      status == GRPC.Status.failed_precondition() -> :ended
      status == GRPC.Status.permission_denied() -> :auth
      status == GRPC.Status.unauthenticated() -> :auth
      status == GRPC.Status.resource_exhausted() -> :rate
      true -> :transient
    end
  end

  defp registry_key(%{channel_id: channel_id, live_chat_id: live_chat_id}) do
    if channel_id, do: {:chat, channel_id}, else: {:pinned, live_chat_id}
  end

  defp registered?(key, pid) do
    case Horde.Registry.lookup(YtIngress.Registry, key) do
      [{^pid, _}] -> true
      _ -> false
    end
  end
end
