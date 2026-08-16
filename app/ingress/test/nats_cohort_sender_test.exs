# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary and unlicensed. See LICENSE.md.

defmodule Ingress.Nats.CohortSenderTest do
  use ExUnit.Case, async: true

  alias Ingress.Nats.CohortSender

  defmodule BarrierGnat do
    use GenServer

    def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

    @impl true
    def init(opts) do
      {:ok,
       %{
         expected: Keyword.fetch!(opts, :expected),
         owner: Keyword.fetch!(opts, :owner),
         calls: []
       }}
    end

    @impl true
    def handle_call({:pub, _subject, _payload, _opts}, from, state) do
      calls = [from | state.calls]

      if length(calls) == state.expected do
        Enum.each(calls, &GenServer.reply(&1, :ok))
        send(state.owner, {:coalesced, length(calls)})
        {:noreply, %{state | calls: []}}
      else
        {:noreply, %{state | calls: calls}}
      end
    end
  end

  test "fans one cohort into simultaneous public Gnat calls" do
    {:ok, connection} = start_supervised({BarrierGnat, expected: 3, owner: self()})
    senders = CohortSender.start(3)
    on_exit(fn -> CohortSender.stop(senders) end)

    requests =
      for id <- 1..3 do
        {id, "twitch.ingress.event.standard", "{}", reply_to: "_INBOX.#{id}"}
      end

    assert CohortSender.publish(senders, connection, requests, 1_000) |> Enum.sort() == [
             {1, :ok},
             {2, :ok},
             {3, :ok}
           ]

    assert_received {:coalesced, 3}
  end

  test "a wedged connection costs one timeout for the whole lane, not one per write" do
    {:ok, connection} = start_supervised({BarrierGnat, expected: 99, owner: self()})
    senders = CohortSender.start(1)
    on_exit(fn -> CohortSender.stop(senders) end)

    requests =
      for id <- 1..4 do
        {id, "twitch.ingress.event.standard", "{}", reply_to: "_INBOX.#{id}"}
      end

    started = System.monotonic_time(:millisecond)
    results = CohortSender.publish(senders, connection, requests, 200)
    elapsed = System.monotonic_time(:millisecond) - started

    # The barrier never replies, so the first write times out and the rest of
    # the lane fails closed without calling: the collector is blocked here, so
    # four writes must not cost four timeouts.
    assert Enum.sort(results) == [
             {1, {:error, :not_connected}},
             {2, {:error, :not_connected}},
             {3, {:error, :not_connected}},
             {4, {:error, :not_connected}}
           ]

    assert elapsed < 600
  end
end
