# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TestSupport.FakeGnat do
  @moduledoc """
  Stand-in for a Gnat connection process: allocates sequential sids like Gnat
  does, records every sub/unsub, and can fail the nth sub. Test-only.
  """
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: opts[:name])

  def calls(pid), do: GenServer.call(pid, :calls)

  @impl true
  def init(opts), do: {:ok, %{fail_sub_at: opts[:fail_sub_at], next_sid: 1, calls: []}}

  @impl true
  def handle_call({:sub, _receiver, topic, _opts}, _from, state) do
    state = %{state | calls: [{:sub, topic} | state.calls]}

    if state.next_sid == state.fail_sub_at do
      {:reply, {:error, :boom}, %{state | next_sid: state.next_sid + 1}}
    else
      {:reply, {:ok, state.next_sid}, %{state | next_sid: state.next_sid + 1}}
    end
  end

  def handle_call({:unsub, sid, _opts}, _from, state) do
    {:reply, :ok, %{state | calls: [{:unsub, sid} | state.calls]}}
  end

  def handle_call(:calls, _from, state), do: {:reply, Enum.reverse(state.calls), state}
end
