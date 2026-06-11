defmodule Ingress.ShardSession do
  @moduledoc """
  Owns one EventSub WebSocket shard of the Conduit. One GenServer per shard,
  registered cluster-wide in `Ingress.Registry` under `{:shard, shard_id}`,
  supervised by `Ingress.ShardSupervisor` (Horde) so ownership moves to a
  surviving node when this one dies.

  The EventSub protocol obligations this process encodes:

    * **Fresh connect**: open the socket, wait for `session_welcome`, then bind
      the new `session_id` to our shard on the Conduit via Helix. Twitch sends
      no events to a shard until the bind succeeds.

    * **No zombie connections**: `session_welcome` carries
      `keepalive_timeout_seconds`. Every inbound frame re-arms a watchdog
      timer; if neither an event nor a `session_keepalive` arrives within the
      window (plus grace) the socket is presumed dead, torn down, and the
      shard reconnects with backoff. We never sit on a silent socket.

    * **Never skip `session_reconnect`**: when Twitch asks us to move, we open
      a *second* socket to the provided `reconnect_url` while keeping the old
      one delivering events, and only after the new socket's
      `session_welcome` do we close the old one. The session id is preserved,
      so no re-bind is needed. If the handshake does not complete within a
      deadline we fall back to a full fresh reconnect (which does re-bind).

  Restart strategy is `:transient`: a crash restarts the shard (fresh connect
  path heals it), a deliberate shutdown does not.
  """

  use GenServer, restart: :transient
  require Logger

  alias Ingress.{Config, Metrics, Nats, Pipeline, WS}
  alias Ingress.Twitch.Api

  # How long a fresh socket may take to deliver session_welcome.
  @welcome_deadline_ms 15_000
  # Slack on top of Twitch's keepalive_timeout_seconds before we call zombie.
  @keepalive_grace_ms 5_000
  # How long the reconnect handshake may take before we give up on the new
  # socket and do a full fresh reconnect.
  @handshake_deadline_ms 30_000
  @base_backoff_ms 1_000
  @max_backoff_ms 60_000

  defstruct shard_id: nil,
            conduit_id: nil,
            # active socket (may still be pre-welcome on fresh connect)
            primary: nil,
            # replacement socket during the session_reconnect handshake
            pending: nil,
            session_id: nil,
            keepalive_ms: nil,
            bound?: false,
            watchdog: nil,
            welcome_timer: nil,
            handshake_timer: nil,
            attempts: 0

  def start_link(opts) do
    shard_id = Keyword.fetch!(opts, :shard_id)
    GenServer.start_link(__MODULE__, opts, name: via(shard_id))
  end

  def via(shard_id), do: {:via, Horde.Registry, {Ingress.Registry, {:shard, shard_id}}}

  def child_spec(opts) do
    %{
      id: {:shard, Keyword.fetch!(opts, :shard_id)},
      start: {__MODULE__, :start_link, [opts]},
      restart: :transient
    }
  end

  @impl true
  def init(opts) do
    Process.flag(:trap_exit, true)
    shard_id = Keyword.fetch!(opts, :shard_id)
    Logger.metadata(shard_id: shard_id)

    state = %__MODULE__{
      shard_id: shard_id,
      conduit_id: Keyword.fetch!(opts, :conduit_id)
    }

    {:ok, state, {:continue, :connect}}
  end

  @impl true
  def handle_continue(:connect, state), do: {:noreply, connect(state)}

  @impl true
  def handle_info(:retry_connect, state), do: {:noreply, connect(state)}

  def handle_info(:welcome_deadline, %{session_id: nil} = state) do
    Logger.warning("no session_welcome within deadline; reconnecting")
    {:noreply, reconnect(state)}
  end

  def handle_info(:welcome_deadline, state), do: {:noreply, state}

  def handle_info(:keepalive_timeout, state) do
    Logger.warning("keepalive window elapsed; zombie connection, reconnecting")
    Metrics.count("Shard/ZombieTimeouts")
    {:noreply, reconnect(state)}
  end

  def handle_info(:handshake_deadline, %{pending: pending} = state) when pending != nil do
    Logger.warning("session_reconnect handshake did not complete; full reconnect")
    {:noreply, reconnect(state)}
  end

  def handle_info(:handshake_deadline, state), do: {:noreply, state}

  def handle_info(message, state) do
    case route(message, state) do
      :unknown ->
        # A message from a socket we already discarded.
        {:noreply, state}

      {:noreply, _} = reply ->
        reply

      {:stop, _, _} = stop ->
        stop
    end
  end

  # Try the pending socket first (reconnect handshake in flight), then primary.
  defp route(message, state) do
    case WS.stream(state.pending, message) do
      :unknown ->
        case WS.stream(state.primary, message) do
          :unknown -> :unknown
          result -> apply_stream(:primary, result, state)
        end

      result ->
        apply_stream(:pending, result, state)
    end
  end

  defp apply_stream(which, {:ok, ws, events}, state) do
    state = put_socket(state, which, ws)
    handle_events(which, events, state)
  end

  defp apply_stream(which, {:error, ws, reason, events}, state) do
    state = put_socket(state, which, ws)

    case handle_events(which, events, state) do
      {:noreply, state} -> socket_down(which, reason, state)
      other -> other
    end
  end

  defp put_socket(state, :primary, ws), do: %{state | primary: ws}
  defp put_socket(state, :pending, ws), do: %{state | pending: ws}

  defp handle_events(_which, [], state), do: {:noreply, state}

  defp handle_events(which, [event | rest], state) do
    case handle_event(which, event, state) do
      {:noreply, state} -> handle_events(which, rest, state)
      other -> other
    end
  end

  defp handle_event(_which, :upgraded, state), do: {:noreply, state}

  defp handle_event(which, {:frame, {:text, data}}, state) do
    case Jason.decode(data) do
      {:ok, message} ->
        handle_twitch(which, message, state)

      {:error, reason} ->
        # Crash on a malformed payload: the supervisor restarts us and Twitch
        # redelivers through the Conduit. No retry storm on bad payloads.
        {:stop, {:bad_payload, reason}, state}
    end
  end

  defp handle_event(which, {:frame, {:ping, payload}}, state) do
    state =
      case WS.send_frame(socket(state, which), {:pong, payload}) do
        {:ok, ws} -> put_socket(state, which, ws)
        {:error, _} -> state
      end

    {:noreply, pet_watchdog(state)}
  end

  defp handle_event(_which, {:frame, {:pong, _}}, state), do: {:noreply, pet_watchdog(state)}

  defp handle_event(which, {:frame, {:close, code, reason}}, state) do
    socket_down(which, {:remote_close, code, reason}, state)
  end

  defp handle_event(which, {:closed, reason}, state), do: socket_down(which, reason, state)

  defp handle_event(_which, _event, state), do: {:noreply, state}

  defp socket(state, :primary), do: state.primary
  defp socket(state, :pending), do: state.pending

  # --- EventSub protocol messages -------------------------------------------

  defp handle_twitch(which, %{"metadata" => %{"message_type" => type}} = message, state) do
    handle_twitch(which, type, message["payload"] || %{}, message["metadata"], state)
  end

  defp handle_twitch(_which, message, state) do
    Logger.warning("frame without metadata: #{inspect(message)}")
    {:noreply, pet_watchdog(state)}
  end

  # Welcome on the pending socket: the reconnect handshake completes. Promote
  # it, close the old socket, keep the session (same session_id, binding and
  # subscriptions carry over).
  defp handle_twitch(:pending, "session_welcome", payload, _meta, state) do
    session = payload["session"] || %{}
    cancel(state.handshake_timer)
    WS.close(state.primary)

    Logger.info("reconnect handshake complete, session #{session["id"]} moved")

    state = %{
      state
      | primary: state.pending,
        pending: nil,
        handshake_timer: nil,
        session_id: session["id"] || state.session_id,
        keepalive_ms: keepalive_ms(session, state),
        attempts: 0
    }

    {:noreply, pet_watchdog(state)}
  end

  # Welcome on a fresh primary socket: bind the new session to our shard on
  # the Conduit. Twitch routes nothing to the shard until this succeeds.
  defp handle_twitch(:primary, "session_welcome", payload, _meta, state) do
    session = payload["session"] || %{}
    session_id = session["id"]
    cancel(state.welcome_timer)

    state = %{
      state
      | welcome_timer: nil,
        session_id: session_id,
        keepalive_ms: keepalive_ms(session, state)
    }

    case Api.assign_shard(state.conduit_id, state.shard_id, session_id) do
      :ok ->
        Logger.info("shard bound, session #{session_id}")
        Metrics.event("ShardUp", %{shard_id: state.shard_id, session_id: session_id})

        Nats.publish("twitch.ingress.status.shard.up", %{
          shard_id: state.shard_id,
          node: node(),
          session_id: session_id,
          since: DateTime.utc_now()
        })

        {:noreply, pet_watchdog(%{state | bound?: true, attempts: 0})}

      {:error, reason} ->
        Logger.error("shard bind failed: #{inspect(reason)}; reconnecting")
        {:noreply, reconnect(state)}
    end
  end

  defp handle_twitch(_which, "session_keepalive", _payload, _meta, state) do
    {:noreply, pet_watchdog(state)}
  end

  # Twitch is moving the session. Open the replacement socket but keep the old
  # one until the new welcome arrives; events keep flowing on the old socket
  # during the handshake. This must never be skipped or shortcut.
  defp handle_twitch(which, "session_reconnect", payload, _meta, state) do
    url = get_in(payload, ["session", "reconnect_url"])
    Logger.info("session_reconnect requested (on #{which} socket)")
    Metrics.count("Shard/SessionReconnects")

    # A reconnect for an already-superseded handshake: drop the stale pending.
    if state.pending, do: WS.close(state.pending)
    cancel(state.handshake_timer)

    case url && WS.connect(url) do
      {:ok, pending} ->
        timer = Process.send_after(self(), :handshake_deadline, @handshake_deadline_ms)
        {:noreply, pet_watchdog(%{state | pending: pending, handshake_timer: timer})}

      other ->
        Logger.warning("reconnect_url connect failed (#{inspect(other)}); full reconnect")
        {:noreply, reconnect(%{state | pending: nil, handshake_timer: nil})}
    end
  end

  defp handle_twitch(_which, "notification", payload, meta, state) do
    Pipeline.handle_event(payload, %{
      shard_id: state.shard_id,
      msg_id: meta["message_id"],
      ts: meta["message_timestamp"]
    })

    {:noreply, pet_watchdog(state)}
  end

  defp handle_twitch(_which, "revocation", payload, _meta, state) do
    Logger.warning("subscription revoked: #{inspect(payload["subscription"])}")
    {:noreply, pet_watchdog(state)}
  end

  defp handle_twitch(_which, type, _payload, _meta, state) do
    Logger.debug("unhandled eventsub message_type #{type}")
    {:noreply, pet_watchdog(state)}
  end

  defp keepalive_ms(session, state) do
    case session["keepalive_timeout_seconds"] do
      s when is_integer(s) and s > 0 -> s * 1000
      _ -> state.keepalive_ms || 10_000
    end
  end

  # --- socket loss -----------------------------------------------------------

  # The old socket dying while a handshake is in flight is expected (Twitch
  # closes it after the grace period); keep waiting on the pending socket.
  defp socket_down(:primary, reason, %{pending: pending} = state) when pending != nil do
    Logger.info("old socket closed during reconnect handshake: #{inspect(reason)}")
    WS.close(state.primary)
    {:noreply, %{state | primary: nil}}
  end

  defp socket_down(:primary, reason, state) do
    Logger.warning("socket down: #{inspect(reason)}; reconnecting")
    {:noreply, reconnect(state)}
  end

  defp socket_down(:pending, reason, state) do
    Logger.warning("replacement socket failed during handshake: #{inspect(reason)}")
    cancel(state.handshake_timer)
    {:noreply, reconnect(%{state | pending: nil, handshake_timer: nil})}
  end

  # --- connect / reconnect ---------------------------------------------------

  defp connect(state) do
    case WS.connect(Config.eventsub_url()) do
      {:ok, ws} ->
        timer = Process.send_after(self(), :welcome_deadline, @welcome_deadline_ms)
        %{state | primary: ws, welcome_timer: timer}

      {:error, reason} ->
        Logger.warning("connect failed: #{inspect(reason)}")
        schedule_retry(state)
    end
  end

  # Full fresh reconnect: tear everything down, new session, re-bind via Helix.
  defp reconnect(state) do
    if state.bound? do
      Metrics.event("ShardDown", %{shard_id: state.shard_id, reason: "reconnecting"})

      Nats.publish("twitch.ingress.status.shard.down", %{
        shard_id: state.shard_id,
        node: node(),
        reason: "reconnecting"
      })
    end

    state |> teardown() |> schedule_retry()
  end

  defp teardown(state) do
    WS.close(state.primary)
    WS.close(state.pending)
    Enum.each([state.watchdog, state.welcome_timer, state.handshake_timer], &cancel/1)

    %{
      state
      | primary: nil,
        pending: nil,
        session_id: nil,
        bound?: false,
        watchdog: nil,
        welcome_timer: nil,
        handshake_timer: nil
    }
  end

  defp schedule_retry(state) do
    attempts = state.attempts + 1
    backoff = min(@base_backoff_ms * Integer.pow(2, min(attempts - 1, 6)), @max_backoff_ms)
    delay = backoff + :rand.uniform(1_000)
    Logger.info("reconnecting in #{delay}ms (attempt #{attempts})")
    Metrics.count("Shard/Reconnects")
    Process.send_after(self(), :retry_connect, delay)
    %{state | attempts: attempts}
  end

  # Every inbound message proves the socket is alive; re-arm the watchdog.
  defp pet_watchdog(state) do
    cancel(state.watchdog)
    window = (state.keepalive_ms || 10_000) + @keepalive_grace_ms
    %{state | watchdog: Process.send_after(self(), :keepalive_timeout, window)}
  end

  defp cancel(nil), do: :ok
  defp cancel(ref), do: Process.cancel_timer(ref)

  @impl true
  def terminate(_reason, state) do
    if state.bound? do
      Metrics.event("ShardDown", %{shard_id: state.shard_id, reason: "terminating"})

      Nats.publish("twitch.ingress.status.shard.down", %{
        shard_id: state.shard_id,
        node: node(),
        reason: "terminating"
      })
    end

    WS.close(state.primary)
    WS.close(state.pending)
    :ok
  end
end
