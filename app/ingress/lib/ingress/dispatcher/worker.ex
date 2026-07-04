defmodule Ingress.Dispatcher.Worker do
  use GenServer

  require Logger

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(_opts) do
    send(Ingress.Dispatcher, {:worker_ready, self()})
    {:ok, %{}}
  end

  @impl true
  def handle_cast({:process, payload, meta}, state) do
    try do
      Ingress.Pipeline.handle_event(payload, meta)
    catch
      kind, reason ->
        Logger.error("Worker pipeline failed: #{kind} #{inspect(reason)}")
    after
      send(Ingress.Dispatcher, {:worker_ready, self()})
    end

    {:noreply, state}
  end
end
