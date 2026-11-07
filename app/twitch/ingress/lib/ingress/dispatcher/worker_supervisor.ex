# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Dispatcher.WorkerSupervisor do
  use Supervisor

  alias Ingress.Config.Dispatcher, as: DispatcherConfig

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: name(opts))
  end

  @spec start_worker(keyword(), non_neg_integer()) :: :ok
  def start_worker(opts, index) do
    case Supervisor.start_child(name(opts), worker_spec(opts, index)) do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _pid}} -> :ok
    end
  end

  @spec stop_worker(keyword(), non_neg_integer()) :: :ok
  def stop_worker(opts, index) do
    :ok = Supervisor.terminate_child(name(opts), {:worker, index})
    Supervisor.delete_child(name(opts), {:worker, index})
  end

  @spec running_indices(keyword()) :: [non_neg_integer()]
  def running_indices(opts) do
    for {{:worker, index}, _pid, _type, _modules} <- Supervisor.which_children(name(opts)) do
      index
    end
  end

  @impl true
  def init(opts) do
    {floor, _ceiling} = DispatcherConfig.pool_bounds(opts)
    children = for index <- 0..(floor - 1), do: worker_spec(opts, index)
    Supervisor.init(children, strategy: :one_for_one)
  end

  defp name(opts), do: Keyword.get(opts, :worker_supervisor_name, __MODULE__)

  defp worker_spec(opts, index) do
    dispatcher = Keyword.get(opts, :name, Ingress.Dispatcher)

    worker_opts = [
      index: index,
      name: elem(Ingress.Dispatcher.worker_names(dispatcher), index),
      dispatcher: dispatcher,
      handler: Keyword.get(opts, :handler, &Ingress.Pipeline.handle_event/2),
      completion_batch_size:
        Keyword.get(opts, :completion_batch_size, DispatcherConfig.completion_batch_size()),
      completion_flush_ms:
        Keyword.get(opts, :completion_flush_ms, DispatcherConfig.completion_flush_ms())
    ]

    Supervisor.child_spec({Ingress.Dispatcher.Worker, worker_opts}, id: {:worker, index})
  end
end
