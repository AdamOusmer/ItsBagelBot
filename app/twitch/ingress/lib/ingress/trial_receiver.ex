# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialReceiver do
  use GenServer
  require Logger

  alias Ingress.{Dispatcher, JSON, Rpc, TrialRpc, Trials, WS}

  @tick_ms 5_000
  @reconnect_ms 2_000
  @welcome_ms 15_000
  @handshake_ms 30_000

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    send(self(), :tick)

    {:ok,
     %{
       owner: inspect({node(), self()}),
       epoch: nil,
       socket: nil,
       pending: nil,
       session_id: nil,
       session_aliases: MapSet.new(),
       rows: %{},
       connected_at: nil,
       pending_at: nil,
       last_frame_at: nil,
       keepalive_ms: 35_000,
       ws: Keyword.get(opts, :ws_module, WS)
     }}
  end

  @impl true
  def handle_info(:tick, state) do
    state = ownership_tick(state)
    Process.send_after(self(), :tick, @tick_ms)
    {:noreply, state}
  end

  def handle_info(:reconnect, state), do: {:noreply, connect(state)}

  def handle_info(message, state) do
    case state.ws.stream(state.socket, message) do
      {:ok, socket, events} ->
        {:noreply, handle_events(events, %{state | socket: socket}, :primary)}

      {:error, socket, _reason, events} ->
        state = handle_events(events, %{state | socket: socket}, :primary)
        {:noreply, fresh_connect(state)}

      :unknown ->
        case state.ws.stream(state.pending, message) do
          {:ok, pending, events} ->
            {:noreply, handle_events(events, %{state | pending: pending}, :pending)}

          {:error, pending, _reason, _events} ->
            state.ws.close(pending)
            {:noreply, %{state | pending: nil}}

          :unknown ->
            {:noreply, state}
        end
    end
  end

  defp ownership_tick(%{epoch: nil} = state) do
    case Trials.acquire(state.owner) do
      {:ok, epoch} -> reconcile(%{state | epoch: epoch})
      _ -> state
    end
  end

  defp ownership_tick(state) do
    case Trials.renew(state.owner, state.epoch) do
      {:ok, 1} -> state |> check_deadlines() |> reconcile()
      _ -> lose_lease(state)
    end
  end

  defp connect(%{epoch: nil} = state), do: state
  defp connect(%{socket: socket} = state) when not is_nil(socket), do: state

  defp connect(state) do
    if Enum.any?(state.rows, fn {_id, row} -> row.enabled and row.state != "stopping" end) do
      url = Application.fetch_env!(:ingress, :eventsub_url)

      case state.ws.connect(url) do
        {:ok, socket} ->
          %{state | socket: socket, connected_at: now_ms(), last_frame_at: now_ms()}

        {:error, reason} ->
          Logger.warning("trial WebSocket connect failed: #{inspect(reason)}")
          Process.send_after(self(), :reconnect, @reconnect_ms)
          state
      end
    else
      state
    end
  end

  defp fresh_connect(state) do
    state.ws.close(state.socket)
    state.ws.close(state.pending)
    if state.epoch, do: Trials.clear_owner_session(state.owner, state.epoch)
    Process.send_after(self(), :reconnect, @reconnect_ms)

    %{
      state
      | socket: nil,
        pending: nil,
        session_id: nil,
        session_aliases: MapSet.new(),
        connected_at: nil,
        pending_at: nil
    }
  end

  defp check_deadlines(state) do
    now = now_ms()

    cond do
      welcome_expired?(state, now) -> fresh_connect(state)
      keepalive_expired?(state, now) -> fresh_connect(state)
      handshake_expired?(state, now) -> fresh_connect(state)
      true -> state
    end
  end

  defp welcome_expired?(state, now),
    do: state.socket && is_nil(state.session_id) && now - state.connected_at > @welcome_ms

  defp keepalive_expired?(state, now),
    do: state.socket && now - state.last_frame_at > state.keepalive_ms

  defp handshake_expired?(state, now),
    do: state.pending && now - state.pending_at > @handshake_ms

  defp now_ms, do: System.monotonic_time(:millisecond)

  defp handle_events(events, state, which) do
    Enum.reduce(events, state, fn event, acc ->
      acc = if which == :primary, do: %{acc | last_frame_at: now_ms()}, else: acc
      handle_event(event, acc, which)
    end)
  end

  defp handle_event({:frame, {:text, data}}, state, which) do
    case JSON.decode(data) do
      {:ok, %{"metadata" => meta, "payload" => payload}} ->
        handle_twitch(meta["message_type"], meta, payload, state, which)

      _ ->
        state
    end
  end

  defp handle_event({:frame, {:ping, payload}}, state, which) do
    socket = if which == :primary, do: state.socket, else: state.pending

    case state.ws.send_frame(socket, {:pong, payload}) do
      {:ok, socket} -> Map.put(state, if(which == :primary, do: :socket, else: :pending), socket)
      _ -> state
    end
  end

  defp handle_event({:frame, {:close, _, _}}, state, :primary), do: fresh_connect(state)
  defp handle_event({:closed, _}, state, :primary), do: fresh_connect(state)
  defp handle_event(_event, state, _which), do: state

  defp handle_twitch("session_welcome", _meta, payload, state, :pending) do
    new_id = get_in(payload, ["session", "id"])

    if !is_binary(new_id) || new_id == "",
      do: lose_lease(state),
      else: accept_pending_welcome(state, new_id)
  end

  defp handle_twitch("session_welcome", _meta, payload, state, :primary) do
    keepalive = get_in(payload, ["session", "keepalive_timeout_seconds"]) || 30
    session_id = get_in(payload, ["session", "id"])

    if !is_binary(session_id) || session_id == "",
      do: lose_lease(state),
      else: accept_primary_welcome(state, session_id, keepalive)
  end

  defp handle_twitch("session_reconnect", _meta, payload, state, :primary) do
    case get_in(payload, ["session", "reconnect_url"]) do
      nil ->
        fresh_connect(state)

      url ->
        case state.ws.connect(url) do
          {:ok, pending} ->
            state.ws.close(state.pending)
            %{state | pending: pending, pending_at: now_ms()}

          _ ->
            fresh_connect(state)
        end
    end
  end

  defp handle_twitch("notification", meta, payload, %{epoch: epoch} = state, :primary)
       when not is_nil(epoch),
       do: handle_trial_notification(meta, payload, state)

  defp handle_twitch("revocation", _meta, payload, state, _which) do
    id = get_in(payload, ["subscription", "condition", "broadcaster_user_id"])
    if row = state.rows[id], do: Trials.fail(id, row.generation, "subscription_revoked")
    state
  end

  defp handle_twitch(_type, _meta, _payload, state, _which), do: state

  defp handle_trial_notification(meta, payload, state) do
    event = payload["event"] || %{}
    id = event["broadcaster_user_id"]
    row = state.rows[id]
    chat_id = event["message_id"]

    if receivable_chat?(row, payload, chat_id) do
      state = update_display_name(state, id, row, event)
      admit_chat(payload, meta, chat_id, row)
      state
    else
      state
    end
  end

  defp receivable_chat?(%{state: "receiving", enabled: true}, payload, chat_id) do
    case get_in(payload, ["subscription", "type"]) do
      "channel.chat.message" -> valid_chat_id?(chat_id)
      _ -> false
    end
  end

  defp receivable_chat?(_row, _payload, _chat_id), do: false
  defp valid_chat_id?(chat_id) when is_binary(chat_id), do: chat_id != ""
  defp valid_chat_id?(_chat_id), do: false

  defp update_display_name(state, id, row, event) do
    case event["broadcaster_user_name"] do
      name when is_binary(name) -> maybe_update_display_name(state, id, row, name)
      _ -> state
    end
  end

  defp maybe_update_display_name(state, _id, _row, ""), do: state

  defp maybe_update_display_name(state, id, row, name) do
    if name == row.display_name do
      state
    else
      case Trials.display_name(id, row.generation, name) do
        {:ok, 1} -> put_in(state, [:rows, id, :display_name], name)
        _ -> state
      end
    end
  end

  defp admit_chat(payload, meta, chat_id, row) do
    id = row.broadcaster_id

    case Trials.admit(id, row.generation, chat_id) do
      :first -> forward_first_chat(payload, meta, id, row)
      :duplicate -> :ok
      :inactive -> :ok
      :unavailable -> Trials.increment(id, "failed")
    end
  end

  defp forward_first_chat(payload, meta, id, row) do
    Dispatcher.dispatch(payload, %{
      shard_id: -1,
      msg_id: meta["message_id"],
      ts: meta["message_timestamp"],
      broadcaster_id: id,
      origin: :trial,
      trial_generation: String.to_integer(row.generation)
    })
  end

  defp accept_pending_welcome(state, new_id) do
    case Trials.owner_session(state.owner, state.epoch, new_id) do
      {:ok, 1} ->
        state.ws.close(state.socket)

        %{
          state
          | socket: state.pending,
            pending: nil,
            pending_at: nil,
            last_frame_at: now_ms(),
            session_id: new_id,
            session_aliases: MapSet.put(state.session_aliases, new_id)
        }

      _ ->
        lose_lease(state)
    end
  end

  defp accept_primary_welcome(state, session_id, keepalive) do
    state = %{
      state
      | session_id: session_id,
        session_aliases: MapSet.new([session_id]),
        keepalive_ms: keepalive * 1_000 + 5_000
    }

    case Trials.owner_session(state.owner, state.epoch, session_id) do
      {:ok, 1} -> reconcile(state)
      _ -> lose_lease(state)
    end
  end

  defp reconcile(state) do
    case Trials.list() do
      {:ok, %{trials: rows}} ->
        rows = Enum.reject(rows, &(&1.state in ["removed", "promoted"]))
        rows = maybe_promote(rows)

        case Enum.reduce_while(rows, :owned, fn row, _ ->
               case Trials.renew(state.owner, state.epoch) do
                 {:ok, 1} ->
                   reconcile_row(row, state)
                   {:cont, :owned}

                 _ ->
                   {:halt, :lost}
               end
             end) do
          :owned -> refresh_rows(state)
          :lost -> lose_lease(state)
        end

      {:error, _} ->
        fresh_connect(%{state | rows: %{}})
    end
  end

  defp refresh_rows(state) do
    case Trials.list() do
      {:ok, %{trials: rows}} ->
        rows = Enum.reject(rows, &(&1.state in ["removed", "promoted"]))

        state
        |> Map.put(:rows, Map.new(rows, &{&1.broadcaster_id, &1}))
        |> maybe_close_idle(Enum.filter(rows, &(&1.enabled and &1.state != "stopping")))
        |> connect()

      _ ->
        fresh_connect(%{state | rows: %{}})
    end
  end

  defp maybe_close_idle(%{socket: nil, pending: nil, session_id: nil} = state, []), do: state
  defp maybe_close_idle(state, []), do: close_idle(state)
  defp maybe_close_idle(state, _rows), do: state

  defp close_idle(state) do
    state.ws.close(state.socket)
    state.ws.close(state.pending)
    if state.epoch, do: Trials.clear_owner_session(state.owner, state.epoch)

    %{
      state
      | socket: nil,
        pending: nil,
        session_id: nil,
        session_aliases: MapSet.new(),
        connected_at: nil,
        pending_at: nil
    }
  end

  defp lose_lease(state) do
    state.ws.close(state.socket)
    state.ws.close(state.pending)

    %{
      state
      | epoch: nil,
        socket: nil,
        pending: nil,
        session_id: nil,
        session_aliases: MapSet.new(),
        rows: %{}
    }
  end

  defp maybe_promote(rows) do
    Enum.map(rows, fn row ->
      if row.state != "stopping" && TrialRpc.registered?(row.broadcaster_id) == {:ok, true} do
        Trials.stop(row.broadcaster_id)
        Trials.field(row.broadcaster_id, "stop_reason", "promoted")
        %{row | state: "stopping"}
      else
        row
      end
    end)
  end

  defp reconcile_row(%{state: "stopping"} = row, state) do
    reply =
      subscription_rpc("delete", %{
        broadcaster_id: row.broadcaster_id,
        subscription_id: row.subscription_id || "",
        owner_epoch: state.epoch,
        trial_generation: row.generation
      })

    if reply["deleted"] == true do
      Trials.finish(row.broadcaster_id, row.stop_reason || "removed")
    end
  end

  defp reconcile_row(%{state: "disabled"} = row, state) do
    if row.subscription_id do
      subscription_rpc("delete", %{
        broadcaster_id: row.broadcaster_id,
        subscription_id: row.subscription_id,
        owner_epoch: state.epoch,
        trial_generation: row.generation
      })
    end
  end

  defp reconcile_row(_row, %{session_id: nil}), do: :ok

  defp reconcile_row(row, state) do
    if subscription_needed?(row, state) do
      case TrialRpc.registered?(row.broadcaster_id) do
        {:ok, false} ->
          reply =
            subscription_rpc("create", %{
              broadcaster_id: row.broadcaster_id,
              session_id: state.session_id,
              owner_epoch: state.epoch,
              trial_generation: row.generation
            })

          if error = reply["error"] do
            Trials.fail(row.broadcaster_id, row.generation, error)
          end

        {:ok, true} ->
          Trials.stop(row.broadcaster_id)
          Trials.field(row.broadcaster_id, "stop_reason", "promoted")

        _ ->
          Trials.fail(row.broadcaster_id, row.generation, "registration_check_unavailable")
      end
    end
  end

  defp subscription_needed?(row, state) do
    row.state in ["pending", "failed"] ||
      !MapSet.member?(state.session_aliases, row.session_id) ||
      is_nil(row.subscription_id)
  end

  defp subscription_rpc(verb, fields) do
    request = JSON.encode(Map.put(fields, :version, 1))

    case Rpc.request(:gnat, "bagel.rpc.outgress.trial_subscription." <> verb, request,
           receive_timeout: 4_000
         ) do
      {:ok, %{body: body}} ->
        case JSON.decode(body) do
          {:ok, reply} -> reply
          _ -> %{"error" => "invalid_reply"}
        end

      _ ->
        %{"error" => "outgress_unavailable"}
    end
  catch
    :exit, _ -> %{"error" => "outgress_unavailable"}
  end
end
