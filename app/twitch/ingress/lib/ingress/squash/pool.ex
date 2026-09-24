# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Squash.Pool do
  use Supervisor

  alias Ingress.Config.Squash, as: SquashConfig
  alias Ingress.Squash

  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    count = SquashConfig.partitions()

    names =
      0..(count - 1)
      |> Enum.map(&String.to_atom("#{Squash}.#{&1}"))
      |> List.to_tuple()

    :persistent_term.put({Squash, :partitions}, names)

    children =
      for index <- 0..(count - 1) do
        name = elem(names, index)

        Supervisor.child_spec(
          {Squash, [name: name, table: String.to_atom("#{Squash}.Keys.#{index}")]},
          id: {:squash, index}
        )
      end

    Supervisor.init(children, strategy: :one_for_one)
  end
end
