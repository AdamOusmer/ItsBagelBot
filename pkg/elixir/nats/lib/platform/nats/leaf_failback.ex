# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.LeafFailback do
  @moduledoc "Moves RPC-plane connections back to the node-local leaf once it is healthy. Elixir twin of pkg/bus/failback.go."
  use GenServer
  require Logger

  alias Platform.Nats
  alias Platform.Nats.{Failback, Probe, Settings}

  def child_spec(opts) do
    %{id: Keyword.get(opts, :name, __MODULE__), start: {__MODULE__, :start_link, [opts]}}
  end

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @impl true
  def init(opts) do
    connections = Keyword.fetch!(opts, :connections)
    interval = Failback.interval()

    state = %{
      connections: connections,
      node: Failback.node_name(),
      health_url:
        System.get_env(
          "NATS_LOCAL_LEAF_HEALTH_URL",
          "https://nats-leaf-local.messaging:8222/healthz"
        ),
      cacerts: Keyword.get_lazy(opts, :cacerts, fn -> Settings.ca_certs() || [] end),
      interval: interval,
      required: Failback.required(),
      timeout: Failback.probe_timeout(),
      successes: Map.new(connections, &{&1, 0})
    }

    Process.send_after(self(), :check, Platform.Backoff.uniform_ms(interval))
    {:ok, state}
  end

  @impl true
  def handle_info(:check, %{node: ""} = state), do: schedule(state)

  def handle_info(:check, state) do
    localities = Enum.map(state.connections, &{&1, locality(&1, state.node)})
    local_ready = Enum.any?(localities, &match?({_, :displaced}, &1)) and local_ready?(state)

    {successes, candidate} =
      Enum.reduce(localities, {state.successes, nil}, &count(&1, &2, local_ready, state.required))

    schedule(%{state | successes: recycle(candidate, successes)})
  end

  defp count({name, :local}, {counts, candidate}, _ready, _required),
    do: {Map.put(counts, name, 0), candidate}

  defp count({_name, :unknown}, acc, _ready, _required), do: acc

  defp count({name, :displaced}, {counts, candidate}, false, _required),
    do: {Map.put(counts, name, 0), candidate}

  defp count({name, :displaced}, {counts, candidate}, true, required) do
    streak = Map.get(counts, name, 0) + 1
    next = if is_nil(candidate) and streak >= required, do: name, else: candidate
    {Map.put(counts, name, streak), next}
  end

  defp recycle(nil, successes), do: successes

  defp recycle(name, successes) do
    Logger.info(
      "NATS leaf failback: moving #{name} from #{Nats.server_name(name)} back to the node-local leaf"
    )

    Nats.stop(name)
    Map.put(successes, name, 0)
  end

  defp schedule(state) do
    Process.send_after(self(), :check, state.interval)
    {:noreply, state}
  end

  defp locality(name, node) do
    case Nats.server_name(name) do
      nil -> :unknown
      server -> if String.starts_with?(server, node <> "--"), do: :local, else: :displaced
    end
  end

  defp local_ready?(state), do: Probe.http_ready?(state.health_url, state.timeout, state.cacerts)
end
