# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.NatsFailback do
  use GenServer
  require Logger

  # Never :gnat_bus: it dials the hub directly, so it would be recycled forever as displaced. Ingress.NatsHubFailback covers it.
  @connections [:gnat]

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    interval = positive_env("NATS_FAILBACK_INTERVAL_MS", 30_000)

    state = %{
      node: System.get_env("NODE_NAME", ""),
      health_url:
        System.get_env(
          "NATS_LOCAL_LEAF_HEALTH_URL",
          "https://nats-leaf-local.messaging:8222/healthz"
        ),
      interval: interval,
      required: positive_env("NATS_FAILBACK_SUCCESSES", 3),
      timeout: positive_env("NATS_FAILBACK_PROBE_TIMEOUT_MS", 1_000),
      successes: Map.new(@connections, &{&1, 0})
    }

    Process.send_after(self(), :check, :rand.uniform(interval))
    {:ok, state}
  end

  @impl true
  def handle_info(:check, %{node: ""} = state) do
    schedule(state)
  end

  def handle_info(:check, state) do
    localities = Enum.map(@connections, &{&1, local_connection?(&1, state.node)})

    local_ready =
      Enum.any?(localities, &match?({_, false}, &1)) and
        local_leaf_ready?(state.health_url, state.timeout)

    {successes, candidate} =
      Enum.reduce(localities, {state.successes, nil}, fn {name, locality}, {counts, candidate} ->
        cond do
          locality == true ->
            {Map.put(counts, name, 0), candidate}

          locality == :unknown ->
            {counts, candidate}

          !local_ready ->
            {Map.put(counts, name, 0), candidate}

          true ->
            count = Map.get(counts, name, 0) + 1
            next = if is_nil(candidate) and count >= state.required, do: name, else: candidate
            {Map.put(counts, name, count), next}
        end
      end)

    successes =
      if candidate do
        server = Platform.Nats.server_name(candidate) || "(unavailable)"
        Logger.info("NatsFailback: recycling #{candidate} from #{server} back to node-local leaf")

        Platform.Nats.stop(candidate)
        Map.put(successes, candidate, 0)
      else
        successes
      end

    schedule(%{state | successes: successes})
  end

  defp schedule(state) do
    Process.send_after(self(), :check, state.interval)
    {:noreply, state}
  end

  @spec local_connection?(atom(), String.t()) :: boolean() | :unknown
  defp local_connection?(name, node) do
    case Platform.Nats.server_name(name) do
      nil -> :unknown
      server -> String.starts_with?(server, node <> "--")
    end
  end

  @doc false
  def local_leaf_ready?(health_url, timeout, cacerts \\ fleet_cacerts()),
    do: Platform.Nats.Probe.http_ready?(health_url, timeout, cacerts)

  defp fleet_cacerts do
    case System.get_env("NATS_CA_PEM") do
      pem when is_binary(pem) and pem != "" ->
        for {:Certificate, der, _} <- :public_key.pem_decode(pem), do: der

      _ ->
        []
    end
  end

  defp positive_env(key, fallback) do
    case Integer.parse(System.get_env(key, "")) do
      {value, ""} when value > 0 -> value
      _ -> fallback
    end
  end
end
