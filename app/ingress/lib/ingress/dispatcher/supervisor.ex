# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Dispatcher.Supervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(
      __MODULE__,
      opts,
      name: Keyword.get(opts, :supervisor_name, __MODULE__)
    )
  end

  @impl true
  def init(opts) do
    children = [
      {Ingress.Dispatcher, opts},
      {Ingress.Dispatcher.WorkerSupervisor, opts}
    ]

    Supervisor.init(children, strategy: :rest_for_one)
  end
end
