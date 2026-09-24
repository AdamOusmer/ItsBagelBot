# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.FakeGnat do
  use GenServer

  @drain_limit 10

  def start_link(opts),
    do: GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))

  @impl true
  def init(opts) do
    {:ok,
     %{
       test: Keyword.get(opts, :test),
       mode: Keyword.get(opts, :mode, :reply),
       sid: 0
     }}
  end

  @impl true
  def handle_call({:sub, _receiver, _topic, _opts}, _from, state) do
    {:reply, {:ok, state.sid + 1}, %{state | sid: state.sid + 1}}
  end

  def handle_call({:pub, _topic, _message, _opts}, _from, %{mode: :stalled} = state),
    do: {:noreply, state}

  def handle_call({:pub, topic, message, opts}, from, %{mode: :coalesce} = state) do
    {publishes, callers} = drain([{topic, message, opts}], [from], @drain_limit)
    Enum.each(publishes, fn {t, m, o} -> send(state.test, {:pub, t, m, o}) end)
    Enum.each(callers, &GenServer.reply(&1, :ok))
    {:noreply, state}
  end

  def handle_call({:pub, topic, message, opts}, _from, state) do
    send(state.test, {:pub, topic, message, opts})
    {:reply, :ok, state}
  end

  defp drain(publishes, callers, 0), do: {publishes, callers}

  defp drain(publishes, callers, remaining) do
    receive do
      {:"$gen_call", from, {:pub, topic, message, opts}} ->
        drain([{topic, message, opts} | publishes], [from | callers], remaining - 1)
    after
      0 -> {publishes, callers}
    end
  end
end
