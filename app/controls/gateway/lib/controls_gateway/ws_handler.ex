# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.WsHandler do
  @moduledoc """
  GET /v1/connect. Auth is Bearer-header only, checked before upgrade.
  State is kept small: it survives hibernation, which this handler requests
  on every callback return (measured: ~15 KB/idle connection at that cost).
  """
  @behaviour :cowboy_websocket

  alias ControlsGateway.{Config, Drain, Metrics, Nats, Subjects}

  @frame_limit 65_536
  @idle_timeout_ms 60_000
  @resubscribe_retry_ms 1_000
  @resubscribe_jitter_ms 1_000
  @text_frames_per_second 50

  # Close codes: 4000 expired (client refreshes+reconnects), 4001 takeover,
  # 4003 revoked. 1001/1007/1008 are the standard going-away, invalid-payload
  # and policy-violation codes.
  @close_expired 4000
  @close_takeover 4001
  @close_revoked 4003

  @impl true
  def init(req, _opts) do
    if Drain.draining?() do
      # Stop accepting new connections without touching the shared listener,
      # so /healthz and /readyz stay reachable through the whole drain.
      {:ok, :cowboy_req.reply(503, %{}, "", req), :draining}
    else
      upgrade_or_reject(req)
    end
  end

  defp upgrade_or_reject(req) do
    case authenticate(req) do
      {:ok, claims} ->
        state = %{device_id: claims.device_id, user_id: claims.sub, exp_ms: claims.exp * 1000}
        opts = %{idle_timeout: @idle_timeout_ms, max_frame_size: @frame_limit, compress: false}
        {:cowboy_websocket, req, state, opts}

      {:error, _reason} ->
        {:ok, :cowboy_req.reply(401, %{}, "", req), :unauthorized}
    end
  end

  defp authenticate(req) do
    with {:ok, token} <- bearer_token(req) do
      Config.token_impl().verify(token)
    end
  end

  defp bearer_token(req) do
    case :cowboy_req.header("authorization", req) do
      <<"Bearer ", token::binary>> when token != "" -> {:ok, token}
      _ -> {:error, :missing_bearer}
    end
  end

  @impl true
  def websocket_init(state) do
    conn_id = :crypto.strong_rand_bytes(12) |> Base.url_encode64(padding: false)

    state =
      Map.merge(state, %{
        conn_id: conn_id,
        subs: nil,
        gnat_mon: nil,
        takeover_announced: false,
        rate_second: 0,
        rate_count: 0
      })

    schedule_expiry(state.exp_ms)
    send(self(), :subscribe)
    Metrics.count("connect")
    Registry.register(ControlsGateway.Connections, conn_id, nil)
    if Drain.draining?(), do: send(self(), {:shutdown_close, 0})

    {:ok, state, :hibernate}
  end

  defp schedule_expiry(exp_ms) do
    delay = max(exp_ms - System.system_time(:millisecond), 0)
    Process.send_after(self(), :token_expired, delay)
  end

  @impl true
  def websocket_handle({:text, data}, state) do
    state = charge_text_frame(state)

    if state.rate_count > @text_frames_per_second do
      Metrics.count("rate_limited_close")
      {:reply, {:close, 1008, "too many frames"}, state, :hibernate}
    else
      handle_text(data, state)
    end
  end

  def websocket_handle({:ping, _data}, state), do: {:ok, state, :hibernate}
  def websocket_handle({:pong, _data}, state), do: {:ok, state, :hibernate}

  def websocket_handle(_other, state) do
    Metrics.count("malformed_close")
    {:reply, {:close, 1007, "malformed frame"}, state, :hibernate}
  end

  defp charge_text_frame(%{rate_second: second, rate_count: count} = state) do
    case System.monotonic_time(:second) do
      ^second -> %{state | rate_count: count + 1}
      now -> %{state | rate_second: now, rate_count: 1}
    end
  end

  defp handle_text(data, state) do
    case Jason.decode(data) do
      {:ok, %{"t" => "ping"}} ->
        {:reply, {:text, Jason.encode!(%{"t" => "pong"})}, state, :hibernate}

      {:ok, %{"t" => t} = frame} when is_binary(t) ->
        forward_event(state, frame)
        {:ok, state, :hibernate}

      _ ->
        Metrics.count("malformed_close")
        {:reply, {:close, 1007, "malformed frame"}, state, :hibernate}
    end
  end

  defp forward_event(state, frame) do
    payload = frame |> Map.put("device_id", state.device_id) |> Map.put("user_id", state.user_id)
    Nats.publish(Subjects.device_evt(state.device_id), payload)
  end

  @impl true
  def websocket_info(:subscribe, state), do: subscribe_or_retry(state)

  def websocket_info({:DOWN, ref, :process, _pid, _reason}, %{gnat_mon: ref} = state) do
    schedule_resubscribe()
    {:ok, %{state | gnat_mon: nil, subs: nil}, :hibernate}
  end

  def websocket_info({:msg, %{topic: topic, body: body}}, state) do
    handle_nats_message(topic, body, state)
  end

  def websocket_info(:token_expired, state) do
    Metrics.count("expired_close")
    {:reply, {:close, @close_expired, "token expired"}, state, :hibernate}
  end

  def websocket_info({:shutdown_close, jitter_ms}, state) do
    Process.send_after(self(), :do_shutdown_close, jitter_ms)
    {:ok, state, :hibernate}
  end

  def websocket_info(:do_shutdown_close, state) do
    {:reply, {:close, 1001, "going away"}, state, :hibernate}
  end

  def websocket_info(_other, state), do: {:ok, state, :hibernate}

  defp subscribe_or_retry(state) do
    case Nats.subscribe_device(state.device_id) do
      {:ok, subs} ->
        mon = Process.monitor(subs.gnat)
        state = announce_takeover(%{state | subs: subs, gnat_mon: mon})
        {:ok, state, :hibernate}

      {:error, :not_connected} ->
        schedule_resubscribe()
        {:ok, state, :hibernate}
    end
  end

  defp announce_takeover(%{takeover_announced: true} = state), do: state

  defp announce_takeover(state) do
    payload = %{"conn_id" => state.conn_id}
    announced = Nats.publish(Subjects.device_takeover(state.device_id), payload) == :ok
    %{state | takeover_announced: announced}
  end

  # Jittered so every socket on the shared NATS connection does not resubscribe
  # in the same instant after a reconnect.
  defp schedule_resubscribe do
    delay = @resubscribe_retry_ms + :rand.uniform(@resubscribe_jitter_ms)
    Process.send_after(self(), :subscribe, delay)
  end

  defp handle_nats_message(topic, body, state) do
    cond do
      topic == Subjects.device_cmd(state.device_id) ->
        {:reply, {:text, body}, state, :hibernate}

      topic == Subjects.device_takeover(state.device_id) ->
        handle_takeover(body, state)

      topic == Subjects.revoked(state.device_id) ->
        Metrics.count("revoked_close")
        {:reply, {:close, @close_revoked, "revoked"}, state, :hibernate}

      true ->
        {:ok, state, :hibernate}
    end
  end

  defp handle_takeover(body, state) do
    case Jason.decode(body) do
      {:ok, %{"conn_id" => conn_id}} when conn_id != state.conn_id ->
        Metrics.count("takeover_close")
        {:reply, {:close, @close_takeover, "takeover"}, state, :hibernate}

      _ ->
        {:ok, state, :hibernate}
    end
  end

  @impl true
  def terminate(_reason, _partial_req, :unauthorized), do: :ok
  def terminate(_reason, _partial_req, :draining), do: :ok

  def terminate(_reason, _partial_req, state) do
    Nats.unsubscribe_device(state.subs)
    Metrics.count("disconnect")
    :ok
  end
end
