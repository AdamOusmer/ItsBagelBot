defmodule Ingress.Dispatcher.WorkerSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(
      __MODULE__,
      opts,
      name: Keyword.get(opts, :worker_supervisor_name, __MODULE__)
    )
  end

  @impl true
  def init(opts) do
    max_running = Keyword.get(opts, :max_running, Ingress.Config.Dispatcher.max_running())
    dispatcher = Keyword.get(opts, :name, Ingress.Dispatcher)
    workers = Ingress.Dispatcher.worker_names(dispatcher)
    handler = Keyword.get(opts, :handler, &Ingress.Pipeline.handle_event/2)

    completion_batch_size =
      Keyword.get(opts, :completion_batch_size, Ingress.Config.Dispatcher.completion_batch_size())

    completion_flush_ms =
      Keyword.get(opts, :completion_flush_ms, Ingress.Config.Dispatcher.completion_flush_ms())

    children =
      for index <- 0..(max_running - 1) do
        worker_opts = [
          index: index,
          name: elem(workers, index),
          dispatcher: dispatcher,
          handler: handler,
          completion_batch_size: completion_batch_size,
          completion_flush_ms: completion_flush_ms
        ]

        Supervisor.child_spec({Ingress.Dispatcher.Worker, worker_opts}, id: {:worker, index})
      end

    Supervisor.init(children, strategy: :one_for_one)
  end
end
