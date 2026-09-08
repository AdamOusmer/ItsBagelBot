# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.FakeGnat do
  @moduledoc """
  Stand-in for a Gnat connection process: a registered GenServer that answers
  `{:sub, ...}` and `{:pub, ...}` and forwards every publish to the test.

  Null Object over the connection — the publisher only ever names it, so a
  process that answers the same two calls is enough, and no broker is needed.

  The three behaviours the suites need are one `:mode` option rather than three
  copies of the boilerplate, because copies drifted: the wire suite's fake had
  gained a stalled mode the collector suites could not reach.

    * `:reply` (default) answers every publish immediately.
    * `:stalled` is alive, connected, and never answers — the wedged socket
      write (TLS renegotiation, a full kernel send buffer, the server's slow
      consumer write deadline) that Gnat's own 5s call default would sit
      through. It forwards nothing, so a stalled connection cannot put a
      message in a mailbox a later `refute_receive` reads.
    * `:coalesce` drains the publishes already queued behind the current one
      before answering any of them, the way a real connection batches a burst
      into one socket write. Needed by cases that assert on a whole cohort.
  """

  use GenServer

  # Bound on one drain pass: a cohort under test is a handful of messages, and
  # an unbounded drain would let a busy mailbox starve the reply.
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
