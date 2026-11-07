# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Dispatcher.WorkerTest do
  use ExUnit.Case, async: true

  alias Ingress.Dispatcher
  alias Ingress.Dispatcher.Worker

  @flush_ms 10

  setup_all do
    {:module, Dispatcher} = Code.ensure_loaded(Dispatcher)
    :erlang.trace_pattern({Dispatcher, :complete_batch, 4}, true, [:global])
    :ok
  end

  defp start_worker(opts \\ []) do
    name = :"worker_#{System.unique_integer([:positive])}"

    defaults = [
      name: name,
      dispatcher: self(),
      index: 0,
      handler: fn _payload, _meta -> :ok end,
      completion_batch_size: 1_000,
      completion_flush_ms: @flush_ms
    ]

    spec = %{
      id: name,
      start: {Worker, :start_link, [Keyword.merge(defaults, opts)]},
      restart: :temporary
    }

    pid = start_supervised!(spec)
    assert_receive {:worker_up, 0, ^pid}
    :erlang.trace(pid, true, [:receive, :call])
    pid
  end

  defp process(pid, broadcaster_id) do
    send(pid, {:process, System.monotonic_time(), %{}, %{broadcaster_id: broadcaster_id}})
  end

  test "an idle worker hibernates and still handles the next message" do
    test = self()

    handler = fn _payload, _meta ->
      send(test, {:handled, length(Enum.to_list(1..200_000))})
    end

    pid = start_worker(hibernate_after_ms: 10, handler: handler)

    process(pid, "b1")
    assert_receive {:handled, 200_000}, 500
    assert Ingress.EnvCase.eventually(fn -> heap_words(pid) < 1_000 end)

    process(pid, "b1")
    assert_receive {:handled, 200_000}, 500
  end

  defp heap_words(pid) do
    {:total_heap_size, words} = Process.info(pid, :total_heap_size)
    words
  end

  test "idle worker never wakes itself" do
    pid = start_worker()

    refute_receive {:trace, ^pid, :receive, :flush_completions}, 100
    refute_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, _}}, 0
  end

  test "completion is flushed within the flush window" do
    pid = start_worker()

    process(pid, "b1")
    process(pid, "b1")
    process(pid, "b2")

    assert_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, [_, ^pid, _, 3]}}, 500
  end

  test "worker goes idle again after the flush" do
    pid = start_worker()

    process(pid, "b1")

    assert_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, [_, _, %{"b1" => 1}, 1]}},
                   500

    assert_receive {:trace, ^pid, :receive, :flush_completions}, 500

    refute_receive {:trace, ^pid, :receive, :flush_completions}, 100
    refute_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, _}}, 0
  end

  test "a later completion re-arms the flush" do
    pid = start_worker()

    process(pid, "b1")

    assert_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, [_, _, %{"b1" => 1}, 1]}},
                   500

    assert_receive {:trace, ^pid, :receive, :flush_completions}, 500

    process(pid, "b2")

    assert_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, [_, _, %{"b2" => 1}, 1]}},
                   500
  end

  test "terminate flushes pending completions" do
    pid = start_worker(completion_flush_ms: 60_000)

    process(pid, "b1")
    :sys.get_state(pid)
    refute_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, _}}, 50

    :ok = GenServer.stop(pid)

    assert_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, [_, _, %{"b1" => 1}, 1]}},
                   500
  end

  test "batch size flush is immediate" do
    pid = start_worker(completion_batch_size: 2, completion_flush_ms: 60_000)

    process(pid, "b1")
    process(pid, "b1")

    assert_receive {:trace, ^pid, :call, {Dispatcher, :complete_batch, [_, _, %{"b1" => 2}, 2]}},
                   500
  end
end
