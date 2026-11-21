# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Throttle do
  use GenServer

  @window_ms 60_000
  @limit 10

  def start_link(_opts), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def reserve(ip), do: GenServer.call(__MODULE__, {:reserve, ip})

  def refund(reservation), do: GenServer.cast(__MODULE__, {:refund, reservation})

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call({:reserve, ip}, _from, state) do
    {window, count} = Map.get(state, ip, {nil, 0})

    case {window, count} do
      {nil, 0} ->
        window = make_ref()
        Process.send_after(self(), {:expire, ip, window}, @window_ms)
        {:reply, {:ok, {ip, window}}, Map.put(state, ip, {window, 1})}

      {_, @limit} ->
        {:reply, :limited, state}

      _ ->
        {:reply, {:ok, {ip, window}}, Map.put(state, ip, {window, count + 1})}
    end
  end

  @impl true
  def handle_cast({:refund, {ip, window}}, state) do
    case Map.get(state, ip) do
      {^window, count} -> {:noreply, Map.put(state, ip, {window, count - 1})}
      _ -> {:noreply, state}
    end
  end

  @impl true
  def handle_info({:expire, ip, window}, state) do
    case Map.get(state, ip) do
      {^window, _count} -> {:noreply, Map.delete(state, ip)}
      _ -> {:noreply, state}
    end
  end
end
