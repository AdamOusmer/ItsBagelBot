defmodule Ingress.Squash.Pool do
  @moduledoc """
  Runs one duplicate-cohort owner per online scheduler.

  Cohorts are partitioned by `{broadcaster, text}`, so all updates for one
  cohort remain ordered while unrelated chat floods are aggregated in parallel.
  """

  use Supervisor

  alias Ingress.{Config, Squash}

  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    count = Keyword.get(opts, :partitions, Config.squash_partitions())

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
