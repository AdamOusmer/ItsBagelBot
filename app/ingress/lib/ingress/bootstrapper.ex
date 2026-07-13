defmodule Ingress.Bootstrapper do
  @moduledoc """
  Runs on every node and periodically makes sure both cluster-singleton
  processes are alive somewhere in the cluster:

    * `Ingress.ShardScaler`   — owns the desired shard count and autoscaler.
    * `Ingress.ConduitManager` — reconciles the Conduit and ShardSessions.

  Horde's registry guarantees at most one instance of each; this loop
  guarantees at least one, even right after boot when cluster membership is
  still syncing, or after the node that hosted them disappears.

  "Registered" is not "alive": a registration can wedge on a pid whose node
  died uncleanly, and `start_child` then answers `:already_started` forever
  while nothing runs — with ConduitManager down, no shard healing runs
  anywhere. So an `:already_started` singleton is probed for process
  liveness (not a mailbox call, so a singleton busy in a long reconcile is
  never mistaken for dead); one that stays unreachable across consecutive
  ticks is terminated through the supervisor and restarted by a following
  tick. Two ticks of misses, not one, so a transient netsplit blip is not
  answered with a replacement.

  ShardScaler is ensured first so ConduitManager finds it ready when its
  first reconcile runs. This process is plainly supervised per node and
  shares no cluster state, so it cannot wedge cluster-wide itself.
  """

  use GenServer
  require Logger

  @interval_ms 10_000
  @probe_timeout_ms 5_000
  @max_probe_misses 2

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    send(self(), :ensure)
    {:ok, %{misses: %{}}}
  end

  @impl true
  def handle_info(:ensure, state) do
    misses = state.misses
    misses = ensure_singleton(Ingress.ShardScaler, "shard scaler", misses)
    misses = ensure_singleton(Ingress.ConduitManager, "conduit manager", misses)

    Process.send_after(self(), :ensure, @interval_ms)
    {:noreply, %{state | misses: misses}}
  end

  defp ensure_singleton(module, label, misses) do
    case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, module) do
      {:ok, _pid} ->
        Logger.info("#{label} started on #{node()}")
        Map.delete(misses, module)

      {:error, {:already_started, pid}} ->
        check_responsive(module, label, pid, misses)

      :ignore ->
        misses

      {:error, reason} ->
        Logger.debug("#{label} not started: #{inspect(reason)}")
        misses
    end
  end

  defp check_responsive(module, label, pid, misses) do
    case probe(pid) do
      :ok -> Map.delete(misses, module)
      :unreachable -> record_miss(module, label, pid, misses)
    end
  end

  defp probe(pid) when node(pid) == node() do
    if Process.alive?(pid), do: :ok, else: :unreachable
  end

  defp probe(pid) do
    case :rpc.call(node(pid), Process, :alive?, [pid], @probe_timeout_ms) do
      true -> :ok
      _ -> :unreachable
    end
  end

  defp record_miss(module, label, pid, misses) do
    seen = Map.get(misses, module, 0) + 1

    if seen >= @max_probe_misses do
      Logger.warning("#{label} registered but not alive (#{seen} probes); replacing")
      replace_singleton(label, pid)
      Map.delete(misses, module)
    else
      Logger.warning("#{label} liveness probe failed (#{seen})")
      Map.put(misses, module, seen)
    end
  end

  # Terminate only; the next :ensure tick performs the start. No immediate
  # restart here, so a replace loop cannot run hotter than the tick interval.
  defp replace_singleton(label, pid) do
    case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid) do
      :ok -> :ok
      {:error, reason} -> Logger.warning("#{label} terminate failed: #{inspect(reason)}")
    end
  end
end
