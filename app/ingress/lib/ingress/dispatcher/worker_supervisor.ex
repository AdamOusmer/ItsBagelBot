defmodule Ingress.Dispatcher.WorkerSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    max_running = Keyword.get(opts, :max_running, Ingress.Config.dispatcher_max_running())

    children =
      for i <- 1..max_running do
        Supervisor.child_spec({Ingress.Dispatcher.Worker, []}, id: :"worker_#{i}")
      end

    Supervisor.init(children, strategy: :one_for_one)
  end
end
