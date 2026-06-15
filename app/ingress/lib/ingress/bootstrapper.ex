defmodule Ingress.Bootstrapper do
  @moduledoc """
  Runs on every node and periodically makes sure both cluster-singleton
  processes are alive somewhere in the cluster:

    * `Ingress.ShardScaler`   — owns the desired shard count and autoscaler.
    * `Ingress.ConduitManager` — reconciles the Conduit and ShardSessions.

  Horde's registry guarantees at most one instance of each; this loop
  guarantees at least one, even right after boot when cluster membership is
  still syncing, or after the node that hosted them disappears. ShardScaler
  is ensured first so ConduitManager finds it ready when its first reconcile
  runs.
  """

  use GenServer
  require Logger

  @interval_ms 10_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    send(self(), :ensure)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:ensure, state) do
    ensure_singleton(Ingress.ShardScaler, "shard scaler")
    ensure_singleton(Ingress.ConduitManager, "conduit manager")

    Process.send_after(self(), :ensure, @interval_ms)
    {:noreply, state}
  end

  defp ensure_singleton(module, label) do
    case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, module) do
      {:ok, _pid} -> Logger.info("#{label} started on #{node()}")
      {:error, {:already_started, _pid}} -> :ok
      :ignore -> :ok
      {:error, reason} -> Logger.debug("#{label} not started: #{inspect(reason)}")
    end
  end
end
