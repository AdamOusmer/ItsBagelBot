# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardSession do
  use GenServer, restart: :transient
  require Logger

  alias Ingress.Config.Twitch, as: TwitchConfig
  alias Ingress.{Metrics, Nats, WS}
  alias Ingress.Twitch.Api

  @welcome_deadline_ms 15_000
  @keepalive_grace_ms 5_000
  @handshake_deadline_ms 30_000
  @takeover_deadline_ms 5_000
  @default_keepalive_ms 10_000
  @base_backoff_ms 1_000
  @max_backoff_ms 60_000
  @registry_check_interval_ms 30_000

  defstruct shard_id: nil,
            conduit_id: nil,
            primary: nil,
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
            takeover: nil,
            name_state: :named,
            ws_connect_opts: [],
            load_counter: Ingress.LoadCounter.new()

  def start_link(opts) do
    if Keyword.get(opts, :rescue?, false) do
      GenServer.start_link(__MODULE__, opts)
    else
      GenServer.start_link(__MODULE__, opts, name: via(Keyword.fetch!(opts, :shard_id)))
    end
  end

  def via(shard_id), do: {:via, Horde.Registry, {Ingress.Registry, {:shard, shard_id}}}

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
      conduit_id: Keyword.fetch!(opts, :conduit_id),
      name_state: if(Keyword.get(opts, :rescue?, false), do: :rescue, else: :named),
      ws_connect_opts: Keyword.get(opts, :ws_connect_opts, [])
    }

    schedule_registry_check()
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

  @impl true
  def handle_call(:release_name, _from, state) do
    Horde.Registry.unregister(Ingress.Registry, {:shard, state.shard_id})
    {:reply, :ok, %{state | name_state: :released}}
  end

  # Must run in this process: Horde binds the name to the caller.
  @impl true
  def handle_call(:reclaim_name, _from, state) do
    result = Horde.Registry.register(Ingress.Registry, {:shard, state.shard_id}, nil)
    {:reply, result, %{state | name_state: reclaimed_name_state(result, state.name_state)}}
  end

  defp reclaimed_name_state({:ok, _pid}, _previous), do: :named

  defp reclaimed_name_state({:error, {:already_registered, pid}}, _prev) when pid == self(),
    do: :named

  defp reclaimed_name_state(_result, previous), do: previous

  @impl true
  def handle_cast(:stand_down_duplicate, %{bound?: true} = state), do: reassert_binding(state)
  def handle_cast(:stand_down_duplicate, state), do: {:stop, :normal, stand_down(state)}

  def handle_cast(:reassert_binding, state), do: reassert_binding(state)

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
      name_state: state.name_state,
      node: node(),
      host: System.get_env("NODE_NAME"),
      session_id: state.session_id,
      bound: state.bound?,
      handshake_in_flight: state.pending != nil,
      keepalive_ms: state.keepalive_ms,
      attempts: state.attempts,
      bound_at: state.bound_at,
      last_frame_at: last_frame_at,
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

  def handle_info(:registry_check, state) do
    schedule_registry_check()
    {:noreply, verify_registration(state)}
  end

  def handle_info(:welcome_deadline, %{session_id: nil} = state) do
    Logger.warning("no session_welcome within deadline; reconnecting")
    {:noreply, reconnect(state)}
  end

  def handle_info(:welcome_deadline, state), do: {:noreply, state}

  def handle_info({:keepalive_timeout, token}, state) do
    case state.watchdog do
      {_timer, ^token} ->
        now_mono = System.monotonic_time(:millisecond)
        window = (state.keepalive_ms || @default_keepalive_ms) + @keepalive_grace_ms

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
    Logger.warning("duplicate shard: takeover timed out; standing down")
    {:stop, :normal, stand_down(state)}
  end

  def handle_info(:takeover_deadline, state), do: {:noreply, state}

  def handle_info(message, state) do
    case route(message, state) do
      :unknown ->
        {:noreply, state}

      {:noreply, _} = reply ->
        reply

      {:stop, _, _} = stop ->
        stop
    end
  end

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

  defp handle_twitch(which, %{"metadata" => %{"message_type" => type}} = message, state) do
    handle_twitch(which, type, message["payload"] || %{}, message["metadata"], state)
  end

  defp handle_twitch(_which, message, state) do
    Logger.warning("frame without metadata: #{inspect(message)}")
    {:noreply, pet_watchdog(state)}
  end

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

  # Never skip: the old socket must keep serving until the new welcome arrives.
  defp handle_twitch(which, "session_reconnect", payload, _meta, state) do
    url = get_in(payload, ["session", "reconnect_url"])
    Logger.info("session_reconnect requested (on #{which} socket)")
    Metrics.count("Shard/SessionReconnects")

    if state.pending, do: WS.close(state.pending)
    cancel(state.handshake_timer)

    case url && WS.connect(url, state.ws_connect_opts) do
      {:ok, pending} ->
        timer = Process.send_after(self(), :handshake_deadline, @handshake_deadline_ms)
        {:noreply, pet_watchdog(%{state | pending: pending, handshake_timer: timer})}

      other ->
        Logger.warning("reconnect_url connect failed (#{inspect(other)}); full reconnect")
        {:noreply, reconnect(%{state | pending: nil, handshake_timer: nil})}
    end
  end

  defp handle_twitch(
         _which,
         "notification",
         %{"subscription" => %{"type" => "user.authorization." <> action}} = payload,
         _meta,
         state
       ) do
    publish_authz(action, payload["event"] || %{})
    {:noreply, state |> count_notification() |> pet_watchdog()}
  end

  defp handle_twitch(_which, "notification", payload, meta, state) do
    admission = %{
      shard_id: state.shard_id,
      msg_id: meta["message_id"],
      ts: meta["message_timestamp"],
      broadcaster_id: Ingress.Pipeline.broadcaster_id(payload["event"] || %{})
    }

    dispatch_notification(payload, admission)

    {:noreply, state |> count_notification() |> pet_watchdog()}
  end

  defp handle_twitch(_which, "revocation", payload, _meta, state) do
    sub = payload["subscription"] || %{}
    Logger.warning("subscription revoked: #{inspect(sub)}")
    Metrics.count("Shard/Revocations")

    Nats.publish("twitch.ingress.status.authz.subrevoked", %{
      broadcaster_id: revoked_broadcaster(sub),
      type: sub["type"],
      status: sub["status"],
      at: DateTime.utc_now()
    })

    {:noreply, pet_watchdog(state)}
  end

  defp handle_twitch(_which, type, _payload, _meta, state) do
    Logger.debug("unhandled eventsub message_type #{type}")
    {:noreply, pet_watchdog(state)}
  end

  defp dispatch_notification(payload, admission) do
    case {Ingress.TrialMembership.lookup(admission.broadcaster_id),
          get_in(payload, ["subscription", "type"])} do
      {%{generation: generation}, "channel.chat.message"} ->
        dispatch_trial_notification(payload, admission, generation)

      _ ->
        Ingress.Dispatcher.dispatch(payload, admission)
    end
  end

  defp dispatch_trial_notification(payload, admission, generation) do
    chat_id = get_in(payload, ["event", "message_id"])

    with true <- is_binary(chat_id),
         false <- chat_id == "",
         :first <- Ingress.Trials.admit(admission.broadcaster_id, generation, chat_id) do
      Ingress.Dispatcher.dispatch(
        payload,
        Map.merge(
          admission,
          %{origin: :trial, trial_generation: String.to_integer(generation)}
        )
      )
    else
      _ -> :ok
    end
  end

  @doc false
  def permanent_bind_error?({:shard_errors, errors}) when is_list(errors) do
    Enum.any?(errors, &(&1["code"] == "invalid_parameter"))
  end

  def permanent_bind_error?(_reason), do: false

  defp publish_authz(action, event) when action in ["grant", "revoke"] do
    Metrics.count("Shard/Authz/#{action}")
    outcome = if action == "grant", do: "granted", else: "revoked"

    Nats.publish("twitch.ingress.status.authz.#{outcome}", %{
      user_id: event["user_id"],
      user_login: event["user_login"],
      at: DateTime.utc_now()
    })
  end

  defp publish_authz(action, _event) do
    Logger.debug("unhandled user.authorization action #{action}")
  end

  defp revoked_broadcaster(sub) do
    condition = sub["condition"] || %{}

    condition["broadcaster_user_id"] || condition["to_broadcaster_user_id"] ||
      condition["user_id"]
  end

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
      _ -> state.keepalive_ms || @default_keepalive_ms
    end
  end

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

  defp connect(state) do
    case WS.connect(TwitchConfig.eventsub_url(), state.ws_connect_opts) do
      {:ok, ws} ->
        timer = Process.send_after(self(), :welcome_deadline, @welcome_deadline_ms)
        %{state | primary: ws, welcome_timer: timer}

      {:error, reason} ->
        Logger.warning("connect failed: #{inspect(reason)}")
        schedule_retry(state)
    end
  end

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

  defp schedule_registry_check,
    do: Process.send_after(self(), :registry_check, @registry_check_interval_ms)

  defp verify_registration(%{name_state: :named, takeover: nil} = state) do
    case Horde.Registry.lookup(Ingress.Registry, {:shard, state.shard_id}) do
      [{pid, _}] when pid == self() -> state
      [{_other, _}] -> state
      [] -> reregister(state)
    end
  end

  defp verify_registration(state), do: state

  # Leave duplicates to the reconciler: killing the copy that noticed can close the routed socket.
  defp reregister(state) do
    case Horde.Registry.register(Ingress.Registry, {:shard, state.shard_id}, nil) do
      {:ok, _pid} ->
        Logger.warning("shard registration was missing; re-registered")
        Metrics.count("Shard/RegistrationRepairs")
        state

      {:error, {:already_registered, _pid}} ->
        state
    end
  end

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
        reassert_binding(state)

      {:error, {:already_registered, _pid}} ->
        Logger.warning("duplicate shard: registration reclaimed by another copy; standing down")
        {:stop, :normal, stand_down(state)}
    end
  end

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

  defp count_notification(state) do
    now = System.monotonic_time(:millisecond)
    %{state | load_counter: Ingress.LoadCounter.increment(state.load_counter, now)}
  end

  defp pet_watchdog(state) do
    now_mono = System.monotonic_time(:millisecond)
    now_sys = System.os_time(:millisecond)

    state = %{state | last_frame_mono_ms: now_mono, last_frame_system_ms: now_sys}

    if state.watchdog == nil do
      window = (state.keepalive_ms || @default_keepalive_ms) + @keepalive_grace_ms
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
