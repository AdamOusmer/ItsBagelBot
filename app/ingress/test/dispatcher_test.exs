defmodule Ingress.DispatcherTest do
  use ExUnit.Case, async: true

  alias Ingress.Dispatcher

  defp start_dispatcher(opts \\ []) do
    name = :"dispatcher_#{System.unique_integer([:positive])}"
    test = self()

    handler =
      Keyword.get(opts, :handler, fn payload, meta ->
        send(test, {:processed, self(), payload, meta})

        receive do
          :finish -> :ok
        end
      end)

    defaults = [
      name: name,
      supervisor_name: :"#{name}.Supervisor",
      worker_supervisor_name: :"#{name}.WorkerSupervisor",
      max_running: 2,
      max_queue: 2,
      max_per_broadcaster: 2,
      sweep_ms: 60_000,
      handler: handler
    ]

    start_supervised!({Ingress.Dispatcher.Supervisor, Keyword.merge(defaults, opts)})
    eventually(fn -> workers(name) |> Enum.all?(&(is_pid(&1) and Process.alive?(&1))) end)
    name
  end

  defp workers(dispatcher) do
    dispatcher
    |> Dispatcher.worker_names()
    |> Tuple.to_list()
    |> Enum.map(&Process.whereis/1)
  end

  defp meta(broadcaster_id), do: %{shard_id: 0, broadcaster_id: broadcaster_id}

  defp counts(dispatcher, broadcaster_id) do
    admitted = Dispatcher.admitted_count(dispatcher)

    bc =
      case :ets.lookup(dispatcher, {:bc, broadcaster_id}) do
        [{_, count}] -> count
        [] -> 0
      end

    {admitted, bc}
  end

  defp eventually(fun, attempts \\ 100) do
    cond do
      fun.() -> :ok
      attempts <= 0 -> flunk("condition not met in time")
      true -> Process.sleep(10) && eventually(fun, attempts - 1)
    end
  end

  test "admits under capacity and sends directly to a worker" do
    dispatcher = start_dispatcher()

    assert Dispatcher.dispatch(%{text: "hi"}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{text: "hi"}, %{broadcaster_id: "b1"}}, 500

    send(worker, :finish)
    eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)
  end

  test "drops past pod-wide running plus mailbox capacity" do
    dispatcher =
      start_dispatcher(max_running: 1, max_queue: 0, max_per_broadcaster: 10)

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, _}, 500

    assert Dispatcher.dispatch(%{n: 2}, meta("b1"), dispatcher) == :ok
    refute_receive {:processed, _, %{n: 2}, _}, 100
    assert counts(dispatcher, "b1") == {1, 1}

    send(worker, :finish)
  end

  test "one broadcaster hitting its cap cannot starve another" do
    dispatcher = start_dispatcher(max_running: 2, max_queue: 2, max_per_broadcaster: 1)

    assert Dispatcher.dispatch(%{n: 1}, meta("hot"), dispatcher) == :ok
    assert_receive {:processed, hot_worker, %{n: 1}, _}, 500

    assert Dispatcher.dispatch(%{n: 2}, meta("hot"), dispatcher) == :ok
    refute_receive {:processed, _, %{n: 2}, _}, 100

    assert Dispatcher.dispatch(%{n: 3}, meta("other"), dispatcher) == :ok
    assert_receive {:processed, other_worker, %{n: 3}, %{broadcaster_id: "other"}}, 500

    send(hot_worker, :finish)
    send(other_worker, :finish)
  end

  test "completion frees exact pod and broadcaster counters" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 2, max_per_broadcaster: 1)

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, _}, 500
    assert counts(dispatcher, "b1") == {1, 1}

    send(worker, :finish)
    eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)

    assert Dispatcher.dispatch(%{n: 2}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker2, %{n: 2}, _}, 500
    send(worker2, :finish)
  end

  test "workers release completion accounting in bounded batches" do
    test = self()

    dispatcher =
      start_dispatcher(
        max_running: 1,
        max_queue: 10,
        max_per_broadcaster: 10,
        completion_batch_size: 4,
        completion_flush_ms: 60_000,
        handler: fn payload, _meta -> send(test, {:completed, payload.n}) end
      )

    for n <- 1..3, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)
    for n <- 1..3, do: assert_receive({:completed, ^n}, 500)

    [worker] = workers(dispatcher)
    state = :sys.get_state(worker)
    assert state.completed_total == 3
    assert Dispatcher.admitted_count(dispatcher) == 3

    Dispatcher.dispatch(%{n: 4}, meta("b1"), dispatcher)
    assert_receive {:completed, 4}, 500
    _state = :sys.get_state(worker)

    assert Dispatcher.admitted_count(dispatcher) == 0
    assert counts(dispatcher, "b1") == {0, 0}
  end

  test "pod-wide admission uses atomics rather than a shared ETS counter" do
    dispatcher = start_dispatcher()
    ctx = :persistent_term.get({Dispatcher, :ctx, dispatcher})

    assert is_reference(ctx.admitted)
    assert :ets.lookup(dispatcher, :admitted_count) == []
    assert Dispatcher.admitted_count(dispatcher) == 0
  end

  test "a worker crash reclaims running and queued attributions" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 2, max_per_broadcaster: 3)

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, _}, 500
    assert Dispatcher.dispatch(%{n: 2}, meta("b2"), dispatcher) == :ok
    assert counts(dispatcher, "b1") == {2, 1}

    Process.exit(worker, :kill)

    eventually(fn ->
      counts(dispatcher, "b1") == {0, 0} and counts(dispatcher, "b2") == {0, 0}
    end)
  end

  test "a crash reclaims completions still buffered inside the worker" do
    test = self()

    dispatcher =
      start_dispatcher(
        max_running: 1,
        max_queue: 2,
        completion_batch_size: 4,
        completion_flush_ms: 60_000,
        handler: fn _payload, _meta -> send(test, {:handler_done, self()}) end
      )

    Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher)
    assert_receive {:handler_done, worker}, 500
    state = :sys.get_state(worker)
    assert state.completed_total == 1
    assert counts(dispatcher, "b1") == {1, 1}

    Process.exit(worker, :kill)
    eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)
  end

  test "nil broadcaster IDs use only the pod-wide bound" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 1, max_per_broadcaster: 1)

    assert Dispatcher.dispatch(%{n: 1}, meta(nil), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, %{broadcaster_id: nil}}, 500
    send(worker, :finish)

    eventually(fn -> Dispatcher.admitted_count(dispatcher) == 0 end)
  end

  test "a worker mailbox replaces the central dispatcher queue" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 1, max_per_broadcaster: 2)

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, _}, 500

    assert Dispatcher.dispatch(%{n: 2}, meta("b2"), dispatcher) == :ok
    refute_receive {:processed, _, %{n: 2}, _}, 100
    assert Dispatcher.admitted_count(dispatcher) == 2

    send(worker, :finish)
    assert_receive {:processed, ^worker, %{n: 2}, %{broadcaster_id: "b2"}}, 500
    send(worker, :finish)
  end

  test "the sweep removes zeroed attribution counters" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 1, sweep_ms: 20)

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, _}, 500
    send(worker, :finish)

    eventually(fn -> :ets.lookup(dispatcher, {:bc, "b1"}) == [] end)
  end
end
