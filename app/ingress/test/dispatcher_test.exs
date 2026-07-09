defmodule Ingress.DispatcherTest do
  use ExUnit.Case, async: true

  alias Ingress.Dispatcher

  defp start_dispatcher(opts \\ []) do
    name = :"dispatcher_#{System.unique_integer([:positive])}"

    defaults = [
      name: name,
      max_running: 2,
      max_queue: 2,
      max_per_broadcaster: 2,
      sweep_ms: 60_000
    ]

    start_supervised!({Dispatcher, Keyword.merge(defaults, opts)})
    name
  end

  # Stands in for Ingress.Dispatcher.Worker: speaks the same
  # {:worker_ready, pid} / {:process, payload, meta} protocol without touching
  # Pipeline/NATS. Stays parked after reporting a unit back to the test process
  # until told to :finish, so tests can control exactly when a slot frees up.
  # Deliberately unlinked: one test kills its worker with Process.exit(:kill),
  # which would tear down the test process too if it were spawn_link'd (:kill
  # propagates through links unconditionally, unlike every other exit reason).
  defp spawn_fake_worker(dispatcher, test_pid) do
    spawn(fn -> fake_worker_loop(dispatcher, test_pid) end)
  end

  defp fake_worker_loop(dispatcher, test_pid) do
    send(dispatcher, {:worker_ready, self()})

    receive do
      {:"$gen_cast", {:process, payload, meta}} ->
        send(test_pid, {:processed, self(), payload, meta})

        receive do
          :finish -> fake_worker_loop(dispatcher, test_pid)
        end
    end
  end

  defp meta(broadcaster_id), do: %{shard_id: 0, broadcaster_id: broadcaster_id}

  defp counts(dispatcher, broadcaster_id) do
    admitted = :ets.lookup_element(dispatcher, :admitted_count, 2)

    bc =
      case :ets.lookup(dispatcher, {:bc, broadcaster_id}) do
        [{_, count}] -> count
        [] -> 0
      end

    {admitted, bc}
  end

  defp eventually(fun, attempts \\ 50) do
    cond do
      fun.() ->
        :ok

      attempts <= 0 ->
        flunk("condition not met in time")

      true ->
        Process.sleep(10)
        eventually(fun, attempts - 1)
    end
  end

  test "admits under capacity and dispatches to an idle worker" do
    dispatcher = start_dispatcher()
    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{text: "hi"}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{text: "hi"}, %{broadcaster_id: "b1"}}, 500
  end

  test "drops past pod-wide capacity" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 0, max_per_broadcaster: 10)

    # No worker is registered, so nothing ever frees admitted_count: the
    # second dispatch is strictly over capacity (1 running + 0 queue).
    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert Dispatcher.dispatch(%{n: 2}, meta("b1"), dispatcher) == :ok

    assert counts(dispatcher, "b1") == {1, 1}
  end

  test "one broadcaster hitting its cap is rejected while another broadcaster is still admitted" do
    dispatcher = start_dispatcher(max_running: 10, max_queue: 10, max_per_broadcaster: 1)
    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{n: 1}, meta("hot"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 1}, _}, 500

    # "hot" is now at its cap (1) and that unit is still in flight (the fake
    # worker is parked, has not sent :finish/looped back to ready).
    assert Dispatcher.dispatch(%{n: 2}, meta("hot"), dispatcher) == :ok
    refute_receive {:processed, _, %{n: 2}, _}, 200

    other_worker = spawn_fake_worker(dispatcher, self())
    assert Dispatcher.dispatch(%{n: 3}, meta("other"), dispatcher) == :ok
    assert_receive {:processed, ^other_worker, %{n: 3}, %{broadcaster_id: "other"}}, 500

    send(worker, :finish)
    send(other_worker, :finish)
  end

  test "rejection rolls both counters back to their exact pre-attempt values" do
    dispatcher = start_dispatcher(max_running: 5, max_queue: 5, max_per_broadcaster: 1)
    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 1}, _}, 500
    assert counts(dispatcher, "b1") == {1, 1}

    assert Dispatcher.dispatch(%{n: 2}, meta("b1"), dispatcher) == :ok
    refute_receive {:processed, _, %{n: 2}, _}, 200
    assert counts(dispatcher, "b1") == {1, 1}

    send(worker, :finish)
  end

  test "completing work frees both counters so a capped broadcaster can be admitted again" do
    dispatcher = start_dispatcher(max_running: 5, max_queue: 5, max_per_broadcaster: 1)
    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 1}, _}, 500

    assert Dispatcher.dispatch(%{n: 2}, meta("b1"), dispatcher) == :ok
    refute_receive {:processed, _, %{n: 2}, _}, 200

    send(worker, :finish)
    eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)

    assert Dispatcher.dispatch(%{n: 3}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 3}, _}, 500

    send(worker, :finish)
  end

  test "a worker crash still releases both counters" do
    dispatcher = start_dispatcher(max_running: 5, max_queue: 5, max_per_broadcaster: 1)
    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 1}, _}, 500
    assert counts(dispatcher, "b1") == {1, 1}

    Process.exit(worker, :kill)
    eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)
  end

  test "nil broadcaster_id is never capped and releases without crashing" do
    dispatcher = start_dispatcher(max_running: 5, max_queue: 5, max_per_broadcaster: 1)
    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{n: 1}, meta(nil), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 1}, %{broadcaster_id: nil}}, 500
    send(worker, :finish)

    assert Dispatcher.dispatch(%{n: 2}, meta(nil), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 2}, %{broadcaster_id: nil}}, 500
    send(worker, :finish)
  end

  test "a queued item's broadcaster is correctly attributed once it is later dispatched" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 5, max_per_broadcaster: 5)
    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 1}, _}, 500

    # Only one running slot exists and it's held by b1's in-flight unit, so
    # b2's dispatch lands in the internal queue rather than a worker.
    assert Dispatcher.dispatch(%{n: 2}, meta("b2"), dispatcher) == :ok
    refute_receive {:processed, _, %{n: 2}, _}, 200
    assert counts(dispatcher, "b2") == {2, 1}

    send(worker, :finish)
    assert_receive {:processed, ^worker, %{n: 2}, %{broadcaster_id: "b2"}}, 500

    send(worker, :finish)
  end

  test "the sweep removes a zeroed per-broadcaster entry" do
    dispatcher =
      start_dispatcher(max_running: 5, max_queue: 5, max_per_broadcaster: 5, sweep_ms: 20)

    worker = spawn_fake_worker(dispatcher, self())

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, ^worker, %{n: 1}, _}, 500
    send(worker, :finish)

    eventually(fn -> :ets.lookup(dispatcher, {:bc, "b1"}) == [] end)
  end
end
