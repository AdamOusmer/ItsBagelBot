# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.DispatcherTest do
  use ExUnit.Case, async: true

  import Ingress.EnvCase

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
      scaler_name: :"#{name}.Scaler",
      max_running: 2,
      max_queue: 2,
      max_per_broadcaster: 2,
      sweep_ms: 60_000,
      handler: handler
    ]

    start_supervised!({Ingress.Dispatcher.Supervisor, Keyword.merge(defaults, opts)})
    assert eventually(fn -> workers(name) |> Enum.all?(&(is_pid(&1) and Process.alive?(&1))) end)
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

  test "admits under capacity and sends directly to a worker" do
    dispatcher = start_dispatcher()

    assert Dispatcher.dispatch(%{text: "hi"}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{text: "hi"}, %{broadcaster_id: "b1"}}, 500

    send(worker, :finish)
    assert eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)
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
    assert eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)

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

    assert eventually(fn ->
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
    assert eventually(fn -> counts(dispatcher, "b1") == {0, 0} end)
  end

  test "nil broadcaster IDs use only the pod-wide bound" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 1, max_per_broadcaster: 1)

    assert Dispatcher.dispatch(%{n: 1}, meta(nil), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, %{broadcaster_id: nil}}, 500
    send(worker, :finish)

    assert eventually(fn -> Dispatcher.admitted_count(dispatcher) == 0 end)
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

  describe "elastic pool" do
    defp start_elastic(opts \\ []) do
      name = :"dispatcher_#{System.unique_integer([:positive])}"
      test = self()

      defaults = [
        name: name,
        supervisor_name: :"#{name}.Supervisor",
        worker_supervisor_name: :"#{name}.WorkerSupervisor",
        scaler_name: :"#{name}.Scaler",
        min_running: 2,
        max_running: 16,
        max_queue: 256,
        max_per_broadcaster: 256,
        sweep_ms: 60_000,
        scale_interval_ms: 5,
        shrink_after_ms: 40,
        retire_grace_ms: 10,
        completion_batch_size: 1,
        handler: fn payload, _meta ->
          send(test, {:started, self(), payload})

          receive do
            :finish -> :ok
          end
        end
      ]

      start_supervised!({Ingress.Dispatcher.Supervisor, Keyword.merge(defaults, opts)})
      assert eventually(fn -> Dispatcher.active_workers(name) == defaults[:min_running] end)
      name
    end

    defp alive_workers(dispatcher), do: Enum.count(workers(dispatcher), &is_pid/1)

    defp finish_all(count) do
      for _ <- 1..count do
        assert_receive {:started, worker, _payload}, 1_000
        send(worker, :finish)
      end
    end

    test "starts at the floor and grows with admitted backlog" do
      dispatcher = start_elastic()
      assert alive_workers(dispatcher) == 2

      for n <- 1..40, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)

      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 10 end)
      assert alive_workers(dispatcher) == 10
    end

    test "growth stops at the ceiling" do
      dispatcher = start_elastic()

      for n <- 1..200, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)

      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 16 end)
      Process.sleep(30)
      assert Dispatcher.active_workers(dispatcher) == 16
    end

    test "shrinks back to the floor after the backlog clears and stops the surplus" do
      dispatcher = start_elastic()

      for n <- 1..40, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)
      assert eventually(fn -> alive_workers(dispatcher) == 10 end)

      finish_all(40)

      assert eventually(fn -> Dispatcher.admitted_count(dispatcher) == 0 end)
      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 2 end)
      assert eventually(fn -> alive_workers(dispatcher) == 2 end)
    end

    test "a retiring worker holding work finishes it before it stops" do
      dispatcher = start_elastic()

      for n <- 1..40, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)
      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 10 end)

      for n <- 41..48, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)

      floor_workers = dispatcher |> workers() |> Enum.take(2)
      held = finish_all_but_one(floor_workers, nil)

      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 2 end)
      Process.sleep(50)
      assert Process.alive?(held)
      assert Dispatcher.admitted_count(dispatcher) > 0

      send(held, :finish)
      finish_remaining(dispatcher)

      assert eventually(fn -> alive_workers(dispatcher) == 2 end)
      assert counts(dispatcher, "b1") == {0, 0}
    end

    defp finish_all_but_one(floor_workers, held) do
      receive do
        {:started, worker, _payload} when held == nil ->
          if worker in floor_workers do
            send(worker, :finish)
            finish_all_but_one(floor_workers, nil)
          else
            finish_all_but_one(floor_workers, worker)
          end

        {:started, worker, _payload} ->
          send(worker, :finish)
          finish_all_but_one(floor_workers, held)
      after
        100 -> held
      end
    end

    defp finish_remaining(dispatcher) do
      receive do
        {:started, worker, _payload} ->
          send(worker, :finish)
          finish_remaining(dispatcher)
      after
        100 -> assert Dispatcher.admitted_count(dispatcher) == 0
      end
    end

    test "the pool holds its size while the backlog persists across windows" do
      dispatcher = start_elastic()

      for n <- 1..40, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)
      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 10 end)

      Process.sleep(120)

      assert Dispatcher.active_workers(dispatcher) == 10
      assert alive_workers(dispatcher) == 10
    end

    test "a pod-wide drop still applies at the fixed capacity while the pool is small" do
      dispatcher = start_elastic(max_queue: 2, max_running: 2)

      for n <- 1..10, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)

      assert Dispatcher.admitted_count(dispatcher) == 4
    end

    test "growth reuses a worker that was retiring" do
      dispatcher = start_elastic(retire_grace_ms: 60_000)

      for n <- 1..40, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)
      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 10 end)
      before_shrink = workers(dispatcher)

      finish_all(40)
      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 2 end)
      assert alive_workers(dispatcher) == 10

      for n <- 41..80, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)
      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 10 end)

      assert workers(dispatcher) == before_shrink
    end

    test "a restarted scaler adopts surplus workers and stops them" do
      dispatcher = start_elastic(shrink_after_ms: 60_000)

      for n <- 1..40, do: Dispatcher.dispatch(%{n: n}, meta("b1"), dispatcher)
      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 10 end)
      finish_all(40)
      assert eventually(fn -> Dispatcher.admitted_count(dispatcher) == 0 end)

      Process.exit(Process.whereis(:"#{dispatcher}.Scaler"), :kill)

      assert eventually(fn -> Dispatcher.active_workers(dispatcher) == 2 end)
      assert eventually(fn -> alive_workers(dispatcher) == 2 end)
    end
  end

  test "the sweep removes zeroed attribution counters" do
    dispatcher = start_dispatcher(max_running: 1, max_queue: 1, sweep_ms: 20)

    assert Dispatcher.dispatch(%{n: 1}, meta("b1"), dispatcher) == :ok
    assert_receive {:processed, worker, %{n: 1}, _}, 500
    send(worker, :finish)

    assert eventually(fn -> :ets.lookup(dispatcher, {:bc, "b1"}) == [] end)
  end
end
