# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialMembership do
  use GenServer
  alias Ingress.Trials

  @key {__MODULE__, :rows}
  @refresh_ms 1_000

  def start_link(_opts), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def lookup(id), do: :persistent_term.get(@key, %{})[id]

  @impl true
  def init(_) do
    :persistent_term.put(@key, %{})
    send(self(), :refresh)
    {:ok, %{revision: :unknown}}
  end

  @impl true
  def handle_info(:refresh, state) do
    Process.send_after(self(), :refresh, @refresh_ms)
    {:noreply, refresh(state, Trials.revision())}
  end

  defp refresh(%{revision: revision} = state, {:ok, revision}), do: state

  defp refresh(state, {:ok, revision}) do
    case Trials.list() do
      {:ok, %{trials: rows}} ->
        publish(members(rows))
        %{state | revision: revision}

      _ ->
        state
    end
  end

  defp refresh(state, _unavailable), do: state

  defp members(rows) do
    for row <- rows,
        row.state not in ["removed", "promoted"],
        into: %{},
        do: {row.broadcaster_id, %{generation: row.generation}}
  end

  defp publish(members) do
    if :persistent_term.get(@key, %{}) != members, do: :persistent_term.put(@key, members)
  end
end
