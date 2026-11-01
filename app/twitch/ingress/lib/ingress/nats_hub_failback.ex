# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.NatsHubFailback do
  @moduledoc false
  # Hub (BUS plane) twin of pkg/bus/failback.go, for :gnat_bus and the gnat_bus_pub_* publisher pool.

  use GenServer
  require Logger

  import Bitwise

  alias Ingress.Config
  alias Ingress.Nats.{Publisher, PublisherPool}

  @connection :gnat_bus
  @max_backoff_shift 4

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @impl true
  def init(opts) do
    if Keyword.get_lazy(opts, :enabled, &enabled?/0) do
      # A failed probe dial exits its linked process; it must not take this one down.
      Process.flag(:trap_exit, true)

      interval =
        Keyword.get_lazy(opts, :interval, fn ->
          positive_env("NATS_FAILBACK_INTERVAL_MS", 30_000)
        end)

      timeout =
        Keyword.get_lazy(opts, :timeout, fn ->
          positive_env("NATS_FAILBACK_PROBE_TIMEOUT_MS", 1_000)
        end)

      state = %{
        connections: Keyword.get(opts, :connections, &connections/0),
        probe_settings:
          Keyword.get_lazy(opts, :probe_settings, fn -> probe_settings(timeout) end),
        interval: interval,
        required:
          Keyword.get_lazy(opts, :required, fn -> positive_env("NATS_FAILBACK_SUCCESSES", 3) end),
        local_name: nil,
        checked_at: nil,
        streaks: %{}
      }

      Process.send_after(self(), :check, :rand.uniform(interval))
      {:ok, state}
    else
      :ignore
    end
  end

  @impl true
  def handle_info(:check, state) do
    {state, streaks, candidate} =
      Enum.reduce(state.connections.(), {state, %{}, nil}, fn {name, recycle},
                                                              {state, streaks, candidate} ->
        {locality, state} = locality(state, name)
        streak = Map.get(state.streaks, name, new_streak())

        case observe(streak, locality, state.required) do
          # One move per check, so the shards never all drain at once; the rest keep their streak.
          {true, _next} when not is_nil(candidate) ->
            {state, Map.put(streaks, name, streak), candidate}

          {true, next} ->
            {state, Map.put(streaks, name, next), {name, recycle}}

          {false, next} ->
            {state, Map.put(streaks, name, next), candidate}
        end
      end)

    with {name, recycle} <- candidate do
      Logger.info(
        "NatsHubFailback: moving #{name} from #{server_name(name)} back to node-local hub #{state.local_name}"
      )

      recycle.()
    end

    Process.send_after(self(), :check, state.interval)
    {:noreply, %{state | streaks: streaks}}
  end

  def handle_info({:EXIT, _pid, _reason}, state), do: {:noreply, state}

  @doc false
  # A single publisher shard has nowhere to spill while it drains, so it is left in place.
  def connections do
    pool =
      case :persistent_term.get({Publisher, :n}, 0) do
        n when n > 1 ->
          for index <- 0..(n - 1),
              do: {PublisherPool.connection_name(index), fn -> Publisher.failback(index) end}

        _ ->
          []
      end

    [{@connection, fn -> stop_connection(@connection) end} | pool]
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
    case server_name(name) do
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
      %{state | local_name: probe_server_name(state.probe_settings), checked_at: now}
    end
  end

  @doc false
  @spec probe_server_name(map()) :: String.t() | nil
  def probe_server_name(settings) do
    case Gnat.start_link(settings) do
      {:ok, pid} ->
        name =
          try do
            Gnat.server_info(pid).server_name
          catch
            :exit, _ -> nil
          end

        stop_connection(pid)
        name

      _error ->
        nil
    end
  catch
    :exit, _ -> nil
  end

  defp server_name(connection) do
    if Process.whereis(connection) do
      try do
        Gnat.server_info(connection).server_name
      catch
        :exit, _ -> nil
      end
    end
  end

  defp stop_connection(name_or_pid) do
    try do
      Gnat.stop(name_or_pid)
    catch
      :exit, _ -> :ok
    end
  end

  defp enabled? do
    System.get_env("NODE_NAME", "") != "" and System.get_env("NATS_HUB_HOST", "") != ""
  end

  # Same identity and TLS as gnat_bus, dialed through nats-local (internalTrafficPolicy: Local).
  defp probe_settings(timeout) do
    host = System.get_env("NATS_LOCAL_HUB_HOST", "nats-local.messaging.svc.cluster.local")

    Config.nats_bus()
    |> List.first()
    |> Map.merge(%{host: host, connection_timeout: timeout})
    |> Map.replace_lazy(
      :ssl_opts,
      &Keyword.put(&1, :server_name_indication, String.to_charlist(host))
    )
  end

  defp positive_env(key, fallback) do
    case Integer.parse(System.get_env(key, "")) do
      {value, ""} when value > 0 -> value
      _ -> fallback
    end
  end
end
