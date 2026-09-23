# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialMembership do
  @moduledoc """
  Small replicated read snapshot for the normal Conduit transport. Only a
  broadcaster in the current four-ID trial set pays the Valkey dedup round trip.
  """
  use GenServer
  alias Ingress.Trials

  def start_link(_opts), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def lookup(id), do: :persistent_term.get({__MODULE__, :rows}, %{})[id]

  @impl true
  def init(_) do
    :persistent_term.put({__MODULE__, :rows}, %{})
    send(self(), :refresh)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:refresh, state) do
    case Trials.list() do
      {:ok, %{trials: rows}} ->
        active = Enum.reject(rows, &(&1.state in ["removed", "promoted"]))
        :persistent_term.put({__MODULE__, :rows}, Map.new(active, &{&1.broadcaster_id, &1}))

      # keep the last authoritative snapshot during an outage
      _ ->
        :ok
    end

    Process.send_after(self(), :refresh, 1_000)
    {:noreply, state}
  end
end
