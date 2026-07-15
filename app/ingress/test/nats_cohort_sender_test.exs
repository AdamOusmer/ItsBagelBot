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

    assert CohortSender.publish(senders, connection, requests) |> Enum.sort() == [
             {1, :ok},
             {2, :ok},
             {3, :ok}
           ]

    assert_received {:coalesced, 3}
  end
end
