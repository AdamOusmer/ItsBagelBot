# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialAdmission do
  use GenServer

  alias Ingress.{Dispatcher, Metrics, TrialValkey, Trials}

  @partitions 8
  @batch 256
  @max_queue 20_000

  def partitions, do: @partitions

  def name(partition), do: Module.concat(__MODULE__, "P#{partition}")

  def child_spec(partition) do
    %{id: {__MODULE__, partition}, start: {__MODULE__, :start_link, [partition]}}
  end

  def start_link(partition),
    do: GenServer.start_link(__MODULE__, partition, name: name(partition))

  def submit(payload, %{broadcaster_id: broadcaster_id} = meta, generation) do
    server = Process.whereis(name(:erlang.phash2(broadcaster_id, @partitions)))
    chat_id = get_in(payload, ["event", "message_id"])

    if server && queue_len(server) < @max_queue do
      send(server, {:admit, {broadcaster_id, generation, chat_id, payload, meta}})
    else
      Metrics.count_drop("Trial/AdmissionDropped", "overloaded")
    end

    :ok
  end

  defp queue_len(pid) do
    case Process.info(pid, :message_queue_len) do
      {:message_queue_len, n} -> n
      nil -> @max_queue
    end
  end

  @impl true
  def init(partition), do: {:ok, partition}

  @impl true
  def handle_info({:admit, item}, state) do
    [item] |> drain(@batch - 1) |> admit_batch()
    {:noreply, state}
  end

  defp drain(acc, 0), do: Enum.reverse(acc)

  defp drain(acc, left) do
    receive do
      {:admit, item} -> drain([item | acc], left - 1)
    after
      0 -> Enum.reverse(acc)
    end
  end

  defp admit_batch(items) do
    commands =
      Enum.map(items, fn {id, generation, chat_id, _, _} ->
        Trials.admit_command(id, generation, chat_id)
      end)

    case TrialValkey.pipeline(commands) do
      {:ok, results} ->
        failed = items |> Enum.zip(results) |> Enum.flat_map(&settle/1)
        count_failed(failed)

      {:error, _} ->
        count_failed(Enum.map(items, &elem(&1, 0)))
    end
  end

  defp settle({{id, generation, _chat_id, payload, meta}, result}) do
    case Trials.admit_result(result) do
      :first ->
        Dispatcher.dispatch(
          payload,
          Map.merge(meta, %{origin: :trial, trial_generation: String.to_integer(generation)})
        )

        []

      :unavailable ->
        [id]

      _duplicate_or_inactive ->
        []
    end
  end

  defp count_failed([]), do: :ok

  defp count_failed(ids) do
    ids
    |> Enum.frequencies()
    |> Enum.map(fn {id, n} -> ["HINCRBY", "trial:channel:" <> id, "failed", n] end)
    |> TrialValkey.pipeline()

    :ok
  end
end

defmodule Ingress.TrialAdmission.Pool do
  use Supervisor

  def start_link(opts), do: Supervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    0..(Ingress.TrialAdmission.partitions() - 1)
    |> Enum.map(&{Ingress.TrialAdmission, &1})
    |> Supervisor.init(strategy: :one_for_one)
  end
end
