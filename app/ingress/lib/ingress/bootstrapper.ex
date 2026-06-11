defmodule Ingress.Bootstrapper do
  @moduledoc """
  Runs on every node and periodically makes sure the cluster-singleton
  `Ingress.ConduitManager` is alive somewhere in the cluster. Horde's
  registry guarantees at most one; this loop guarantees at least one, even
  right after boot when cluster membership is still syncing, or after the
  node that hosted it disappears.
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
    case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, Ingress.ConduitManager) do
      {:ok, _pid} -> Logger.info("conduit manager started on #{node()}")
      {:error, {:already_started, _pid}} -> :ok
      :ignore -> :ok
      {:error, reason} -> Logger.debug("conduit manager not started: #{inspect(reason)}")
    end

    Process.send_after(self(), :ensure, @interval_ms)
    {:noreply, state}
  end
end
