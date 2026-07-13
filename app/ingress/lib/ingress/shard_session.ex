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

  alias Ingress.{Config, Metrics, Nats, WS}
  alias Ingress.Twitch.Api

  # How long a fresh socket may take to deliver session_welcome.
  @welcome_deadline_ms 15_000
  # Slack on top of Twitch's keepalive_timeout_seconds before we call zombie.
  @keepalive_grace_ms 5_000
  # How long the reconnect handshake may take before we give up on the new
  # socket and do a full fresh reconnect.
  @handshake_deadline_ms 30_000
  # How long a duplicate-shard takeover may wait for the registry's pick to
  # exit before we yield to it anyway.
  @takeover_deadline_ms 5_000
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
            attempts: 0,
            bound_at: nil,
            last_frame_mono_ms: nil,
            last_frame_system_ms: nil,
            # duplicate-shard takeover in flight: %{winner:, monitor:, timer:}
            takeover: nil,
            # Aggregate counter for notification load.
            load_counter: Ingress.LoadCounter.new()

  # A rescue session (started by the reconciler when the named shard cannot
  # be replaced — its registration or supervision is wedged on a dead pid)
  # runs unnamed: it skips cluster-wide registration entirely, so no wedge
  # can block it, and duplicate-shard resolution never signals it. It serves
  # by binding the shard on Twitch, exactly like a named session, and the
  # reconciler stops it once a named session is serving again.
  def start_link(opts) do
    if Keyword.get(opts, :rescue?, false) do
      GenServer.start_link(__MODULE__, opts)
    else
      GenServer.start_link(__MODULE__, opts, name: via(Keyword.fetch!(opts, :shard_id)))
    end
  end

  def via(shard_id), do: {:via, Horde.Registry, {Ingress.Registry, {:shard, shard_id}}}

  @doc """
  Snapshot of the shard's live state, served from wherever in the cluster the
  shard currently runs. Used by `Ingress.AdminRpc`.
  """
  def status(pid, timeout \\ 2_000), do: GenServer.call(pid, :status, timeout)

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
    if Keyword.get(opts, :rescue?, false), do: Logger.metadata(rescue: true)

    state = %__MODULE__{
      shard_id: shard_id,
      conduit_id: Keyword.fetch!(opts, :conduit_id)
    }

    {:ok, state, {:continue, :connect}}
  end

  @impl true
  def handle_continue(:connect, state), do: {:noreply, connect(state)}

  @impl true
  def handle_call(:status, _from, state) do
    {load, updated_counter} =
      Ingress.LoadCounter.value(state.load_counter, System.monotonic_time(:millisecond))

    state = %{state | load_counter: updated_counter}
    {:reply, status_map(state, load), state}
  end

  # Planned-shutdown handoff (`Ingress.Drain`): give up the cluster-wide
  # registration but keep the socket serving. The successor the drain starts
  # takes the name without a conflict, binds, and only then is this copy
  # stopped — the slot never goes dark. Unregistering also means no
  # name-conflict signal can reach us afterwards, so nothing can order this
  # copy to stand down while it is the one still serving.
  @impl true
  def handle_call(:release_name, _from, state) do
    Horde.Registry.unregister(Ingress.Registry, {:shard, state.shard_id})
    {:reply, :ok, state}
  end

  # The other copy of this shard is bound but lost the registry merge; it is
  # taking the registration over and asks us to stand down. If we bound
  # concurrently (race between the merge and this message), keep serving:
  # the requester's takeover deadline makes it yield instead. Re-assert the
  # binding, since the requester may have bound after us.
  @impl true
  def handle_cast(:stand_down_duplicate, %{bound?: true} = state), do: reassert_binding(state)
  def handle_cast(:stand_down_duplicate, state), do: {:stop, :normal, stand_down(state)}

  # A duplicate-shard resolution just closed the other copy's socket. Twitch
  # routes a shard's events to whichever session bound it last — possibly the
  # copy that stood down — so the survivor re-binds its own session to pull
  # the routing back. The PATCH is idempotent when we already own the binding.
  def handle_cast(:reassert_binding, state), do: reassert_binding(state)

  # Reconciler-ordered repair: Twitch reports this shard's transport dead even
  # though we believe we are bound. Our socket may still be receiving
  # keepalives (Twitch keeps superseded sockets alive), so the watchdog cannot
  # notice; only a full fresh reconnect — new session, new Helix bind — heals.
  def handle_cast(:force_rebind, state) do
    Logger.warning("re-bind forced by reconciler; reconnecting with a fresh session")
    Metrics.count("Shard/ForcedRebinds")
    {:noreply, reconnect(state)}
  end

  defp status_map(state, load) do
    last_frame_at =
      if state.last_frame_system_ms do
        DateTime.from_unix!(state.last_frame_system_ms, :millisecond)
      else
        nil
      end

    %{
      shard_id: state.shard_id,
      state: derive_state(state),
      node: node(),
      # Worker node (machine) name from the downward-API env, so the admin
      # console can show the host instead of the pod IP carried in `node`.
      # Resolved locally here, where the shard actually runs.
      host: System.get_env("NODE_NAME"),
      session_id: state.session_id,
      bound: state.bound?,
      handshake_in_flight: state.pending != nil,
      keepalive_ms: state.keepalive_ms,
      attempts: state.attempts,
      bound_at: state.bound_at,
      last_frame_at: last_frame_at,
      # Notifications received in the last @load_window_ms milliseconds.
      # This is a raw count, not a rate; callers divide by the window if they
      # want events/second. Using the current wall of pruned timestamps avoids
      # a separate timer and stays consistent with the window definition.
      load: load
    }
  end

  defp derive_state(%{bound?: true, pending: pending}) when pending != nil, do: "migrating"
  defp derive_state(%{bound?: true}), do: "connected"
  defp derive_state(%{session_id: id}) when id != nil, do: "binding"
  defp derive_state(%{primary: primary}) when primary != nil, do: "connecting"
  defp derive_state(_state), do: "backoff"

  @impl true
  def handle_info(:retry_connect, state), do: {:noreply, connect(state)}

  def handle_info(:welcome_deadline, %{session_id: nil} = state) do
    Logger.warning("no session_welcome within deadline; reconnecting")
    {:noreply, reconnect(state)}
  end

  def handle_info(:welcome_deadline, state), do: {:noreply, state}

  def handle_info({:keepalive_timeout, token}, state) do
    case state.watchdog do
      {_timer, ^token} ->
        now_mono = System.monotonic_time(:millisecond)
        window = (state.keepalive_ms || 10_000) + @keepalive_grace_ms

        elapsed =
          if state.last_frame_mono_ms, do: now_mono - state.last_frame_mono_ms, else: window

        if elapsed >= window do
          Logger.warning("keepalive window elapsed; zombie connection, reconnecting")
          Metrics.count("Shard/ZombieTimeouts")
          {:noreply, reconnect(%{state | watchdog: nil})}
        else
          remaining = window - elapsed
          new_timer = Process.send_after(self(), {:keepalive_timeout, token}, remaining)
          {:noreply, %{state | watchdog: {new_timer, token}}}
        end

      _ ->
        {:noreply, state}
    end
  end

  def handle_info(:handshake_deadline, %{pending: pending} = state) when pending != nil do
    Logger.warning("session_reconnect handshake did not complete; full reconnect")
    {:noreply, reconnect(state)}
  end

  def handle_info(:handshake_deadline, state), do: {:noreply, state}

  # --- duplicate-shard resolution (netsplit heal) ----------------------------
  #
  # When a netsplit heals, both halves may be running this shard. The Horde
  # registry keeps exactly one registration and sends the other process this
  # exit signal (we trap exits, so it arrives as a message). The registry's
  # pick is arbitrary; ours is not: the copy that is actually serving (bound
  # to the Conduit) survives, the other stands down. Twitch routes a shard's
  # events to whichever socket bound the shard last, so the bound copy is the
  # one receiving traffic.

  def handle_info(
        {:EXIT, _from, {:name_conflict, {{:shard, _id}, _value}, _registry, winner}},
        state
      ) do
    winner_status =
      try do
        GenServer.call(winner, :status, 2_000)
      catch
        :exit, _ -> :unreachable
      end

    cond do
      winner_status == :unreachable ->
        Logger.warning("duplicate shard: registry pick unreachable; reclaiming registration")
        {:noreply, begin_takeover(state, winner)}

      winner_status.bound ->
        Logger.info(
          "duplicate shard resolved: copy on #{winner_status.node} is bound; standing down"
        )

        # If we bound after the winner (rolling deploys race exactly this
        # way), Twitch is routing to the socket we are about to close. The
        # winner re-asserts its binding so the routing follows the survivor.
        GenServer.cast(winner, :reassert_binding)
        {:stop, :normal, stand_down(state)}

      state.bound? ->
        Logger.warning(
          "duplicate shard: we are bound, registry pick on #{winner_status.node} is not; taking over"
        )

        GenServer.cast(winner, :stand_down_duplicate)
        {:noreply, begin_takeover(state, winner)}

      true ->
        Logger.info("duplicate shard resolved: neither copy bound; standing down")
        {:stop, :normal, stand_down(state)}
    end
  end

  def handle_info({:DOWN, ref, :process, _winner, _reason}, %{takeover: %{monitor: ref}} = state) do
    finish_takeover(state)
  end

  def handle_info(:takeover_deadline, %{takeover: takeover} = state) when takeover != nil do
    # The registry's pick did not exit in time (it may have bound in the
    # meantime). Yield to it rather than fight.
    Logger.warning("duplicate shard: takeover timed out; standing down")
    {:stop, :normal, stand_down(state)}
  end

  def handle_info(:takeover_deadline, state), do: {:noreply, state}

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
    case Ingress.JSON.decode(data) do
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

    cancel_watchdog(state.watchdog)
    state = %{state | watchdog: nil}

    publish_bound(state, "moved")

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

    cancel_watchdog(state.watchdog)
    state = %{state | watchdog: nil}

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

        state = %{state | bound?: true, attempts: 0, bound_at: DateTime.utc_now()}
        publish_bound(state, "fresh")

        {:noreply, pet_watchdog(state)}

      {:error, reason} ->
        if permanent_bind_error?(reason) do
          Logger.warning(
            "shard bind rejected as permanent: #{inspect(reason)}; stopping (reconciler restarts the shard only while it fits the conduit)"
          )

          {:stop, :normal, stand_down(state)}
        else
          Logger.error("shard bind failed: #{inspect(reason)}; reconnecting")
          {:noreply, reconnect(state)}
        end
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
    Ingress.Dispatcher.dispatch(payload, %{
      shard_id: state.shard_id,
      msg_id: meta["message_id"],
      ts: meta["message_timestamp"],
      broadcaster_id: Ingress.Pipeline.broadcaster_id(payload["event"] || %{})
    })

    {:noreply, state |> count_notification() |> pet_watchdog()}
  end

  defp handle_twitch(_which, "revocation", payload, _meta, state) do
    Logger.warning("subscription revoked: #{inspect(payload["subscription"])}")
    {:noreply, pet_watchdog(state)}
  end

  defp handle_twitch(_which, type, _payload, _meta, state) do
    Logger.debug("unhandled eventsub message_type #{type}")
    {:noreply, pet_watchdog(state)}
  end

  # A bind rejected with `invalid_parameter` cannot succeed by retrying: the
  # shard id lies outside the conduit's current shard_count, i.e. this shard is
  # excess after a scale-down. Other shard errors (websocket_disconnected,
  # failed ping-pong) are transient session problems a fresh reconnect fixes.
  # Stopping :normal removes the registry entry and, with :transient restart,
  # keeps the supervisor from bringing the shard back on its own.
  @doc false
  def permanent_bind_error?({:shard_errors, errors}) when is_list(errors) do
    Enum.any?(errors, &(&1["code"] == "invalid_parameter"))
  end

  def permanent_bind_error?(_reason), do: false

  # Announce that this shard (re)established its binding: "fresh" after a
  # full connect + Helix bind, "moved" after a session_reconnect handshake
  # carried the session to a new socket. The admin live feed shows these.
  defp publish_bound(state, kind) do
    Nats.publish("twitch.ingress.status.shard.bound", %{
      shard_id: state.shard_id,
      node: node(),
      session_id: state.session_id,
      kind: kind,
      at: DateTime.utc_now()
    })
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

  # --- duplicate-shard takeover helpers --------------------------------------

  # We are keeping the shard: watch the registry's pick until it exits, then
  # reclaim the registration. The deadline bounds the unregistered window.
  defp begin_takeover(state, winner) do
    monitor = Process.monitor(winner)
    timer = Process.send_after(self(), :takeover_deadline, @takeover_deadline_ms)
    %{state | takeover: %{winner: winner, monitor: monitor, timer: timer}}
  end

  defp finish_takeover(%{takeover: %{monitor: monitor, timer: timer}} = state) do
    Process.demonitor(monitor, [:flush])
    cancel(timer)
    state = %{state | takeover: nil}

    case Horde.Registry.register(Ingress.Registry, {:shard, state.shard_id}, nil) do
      {:ok, _} ->
        Logger.info("duplicate shard resolved: registration reclaimed, we keep serving")
        # The loser may have bound after us before it exited; make Twitch's
        # routing follow the copy that actually survived.
        reassert_binding(state)

      {:error, {:already_registered, _pid}} ->
        # A third copy raced us to the name (fresh start by the reconciler).
        Logger.warning("duplicate shard: registration reclaimed by another copy; standing down")
        {:stop, :normal, stand_down(state)}
    end
  end

  # Re-bind our current session to the shard. Twitch's conduit routes to the
  # last session bound, so this is how a surviving copy pulls the routing back
  # after a duplicate resolution. No-op unless we hold a bound session.
  defp reassert_binding(%{bound?: true, session_id: session_id} = state)
       when session_id != nil do
    case Api.assign_shard(state.conduit_id, state.shard_id, session_id) do
      :ok ->
        Logger.info("shard binding re-asserted, session #{session_id}")
        {:noreply, state}

      {:error, reason} ->
        reassert_failed(state, reason)
    end
  end

  defp reassert_binding(state), do: {:noreply, state}

  defp reassert_failed(state, reason) do
    if permanent_bind_error?(reason) do
      Logger.warning("binding re-assert rejected as permanent: #{inspect(reason)}; stopping")
      {:stop, :normal, stand_down(state)}
    else
      Logger.warning("binding re-assert failed: #{inspect(reason)}; reconnecting")
      {:noreply, reconnect(state)}
    end
  end

  # Graceful exit of a redundant duplicate: announce if we were serving, then
  # tear everything down so terminate/1 stays quiet.
  defp stand_down(state) do
    if state.bound? do
      Metrics.event("ShardDown", %{shard_id: state.shard_id, reason: "duplicate_resolved"})

      Nats.publish("twitch.ingress.status.shard.down", %{
        shard_id: state.shard_id,
        node: node(),
        reason: "duplicate_resolved"
      })
    end

    if state.takeover do
      Process.demonitor(state.takeover.monitor, [:flush])
      cancel(state.takeover.timer)
    end

    teardown(%{state | takeover: nil})
  end

  defp teardown(state) do
    WS.close(state.primary)
    WS.close(state.pending)
    cancel_watchdog(state.watchdog)
    Enum.each([state.welcome_timer, state.handshake_timer], &cancel/1)

    %{
      state
      | primary: nil,
        pending: nil,
        session_id: nil,
        bound?: false,
        watchdog: nil,
        welcome_timer: nil,
        handshake_timer: nil,
        bound_at: nil
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

  # Increment the aggregate load counter.
  defp count_notification(state) do
    now = System.monotonic_time(:millisecond)
    %{state | load_counter: Ingress.LoadCounter.increment(state.load_counter, now)}
  end

  # Every inbound message proves the socket is alive.
  defp pet_watchdog(state) do
    now_mono = System.monotonic_time(:millisecond)
    now_sys = System.os_time(:millisecond)

    state = %{state | last_frame_mono_ms: now_mono, last_frame_system_ms: now_sys}

    if state.watchdog == nil do
      window = (state.keepalive_ms || 10_000) + @keepalive_grace_ms
      token = make_ref()
      timer = Process.send_after(self(), {:keepalive_timeout, token}, window)
      %{state | watchdog: {timer, token}}
    else
      state
    end
  end

  defp cancel_watchdog({timer, _}), do: cancel(timer)
  defp cancel_watchdog(nil), do: :ok

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
