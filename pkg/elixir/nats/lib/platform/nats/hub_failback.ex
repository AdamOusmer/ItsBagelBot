# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.HubFailback do
  @moduledoc "Moves hub-plane connections back to the node-local hub. Elixir twin of the hub half of pkg/bus/failback.go."
  use GenServer
  require Logger

  import Bitwise

  alias Platform.Nats
  alias Platform.Nats.Failback

  @max_backoff_shift 4

  def child_spec(opts) do
    %{id: Keyword.get(opts, :name, __MODULE__), start: {__MODULE__, :start_link, [opts]}}
  end

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @doc "Settings that dial the node-local hub with the identity and TLS of `settings`."
  def local_probe_settings(settings, timeout \\ Failback.probe_timeout()) do
    host = System.get_env("NATS_LOCAL_HUB_HOST", "nats-local.messaging.svc.cluster.local")

    settings
    |> Map.merge(%{host: host, connection_timeout: timeout})
    |> Map.replace_lazy(
      :ssl_opts,
      &Keyword.put(&1, :server_name_indication, String.to_charlist(host))
    )
  end

  @impl true
  def init(opts) do
    if Keyword.get_lazy(opts, :enabled, &Failback.enabled?/0) do
      # A failed probe dial exits its linked process; it must not take this one down.
      Process.flag(:trap_exit, true)
      interval = Keyword.get_lazy(opts, :interval, &Failback.interval/0)

      state = %{
        connections: Keyword.fetch!(opts, :connections),
        probe_settings: Keyword.fetch!(opts, :probe_settings),
        interval: interval,
        required: Keyword.get_lazy(opts, :required, &Failback.required/0),
        local_name: nil,
        checked_at: nil,
        streaks: %{}
      }

      Process.send_after(self(), :check, Platform.Backoff.uniform_ms(interval))
      {:ok, state}
    else
      :ignore
    end
  end

  @impl true
  def handle_info(:check, state) do
    {state, streaks, candidate} =
      Enum.reduce(state.connections.(), {state, %{}, nil}, &step/2)

    with {name, recycle} <- candidate do
      Logger.info(
        "NATS hub failback: moving #{name} from #{Nats.server_name(name)} back to node-local hub #{state.local_name}"
      )

      recycle.()
    end

    Process.send_after(self(), :check, state.interval)
    {:noreply, %{state | streaks: streaks}}
  end

  def handle_info({:EXIT, _pid, _reason}, state), do: {:noreply, state}

  defp step({name, recycle}, {state, streaks, candidate}) do
    {locality, state} = locality(state, name)
    streak = Map.get(state.streaks, name, new_streak())

    case observe(streak, locality, state.required) do
      # One move per check, so a pool never drains all at once; the rest keep their streak.
      {true, _next} when not is_nil(candidate) ->
        {state, Map.put(streaks, name, streak), candidate}

      {true, next} ->
        {state, Map.put(streaks, name, next), {name, recycle}}

      {false, next} ->
        {state, Map.put(streaks, name, next), candidate}
    end
  end

  @doc false
  def new_streak, do: %{consecutive: 0, failbacks: 0}

  @doc false
  # A failback that lands remote again doubles the streak it needs next time, capped at 16x.
  @spec observe(map(), :local | :displaced | :unknown, pos_integer()) :: {boolean(), map()}
  def observe(_streak, :local, _required), do: {false, new_streak()}
  def observe(streak, :unknown, _required), do: {false, %{streak | consecutive: 0}}

  def observe(%{consecutive: consecutive, failbacks: failbacks}, :displaced, required) do
    consecutive = consecutive + 1

    if consecutive < required <<< min(failbacks, @max_backoff_shift) do
      {false, %{consecutive: consecutive, failbacks: failbacks}}
    else
      {true, %{consecutive: 0, failbacks: failbacks + 1}}
    end
  end

  defp locality(state, name) do
    case Nats.server_name(name) do
      nil ->
        {:unknown, state}

      connected ->
        state = lookup_local_name(state, connected)

        cond do
          state.local_name in [nil, ""] -> {:unknown, state}
          state.local_name == connected -> {:local, state}
          true -> {:displaced, state}
        end
    end
  end

  # The hub's monitor port stays closed to app namespaces, so locality is read from a client handshake.
  defp lookup_local_name(%{local_name: connected} = state, connected), do: state

  defp lookup_local_name(state, _connected) do
    now = System.monotonic_time(:millisecond)

    if state.checked_at && now - state.checked_at < state.interval do
      state
    else
      %{state | local_name: Nats.probe_server_name(state.probe_settings), checked_at: now}
    end
  end
end
