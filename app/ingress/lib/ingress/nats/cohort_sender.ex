defmodule Ingress.Nats.CohortSender do
  @moduledoc """
  Fixed send-lane pool for one ingress publisher connection.

  `Gnat` can coalesce concurrent `pub/4` calls into one socket write, but a
  cohort collector calling it serially never gives that coalescer more than
  one command. This pool keeps a small set of persistent processes and fans a
  flushed cohort across them. The public Gnat API remains the only wire
  implementation; this module only supplies the concurrency it is designed to
  combine.

  The pool is used for ordinary per-message publishes. Atomic batches retain
  their single ordered sender because their sequence headers must reach NATS
  in order.

  Every lane write is bounded by the caller's `timeout`, and a lane that hits
  one failure fails the rest of its requests without calling again. Both rules
  exist for the same reason: the collector blocks in `publish/4` until every
  lane answers, applying no PubAcks and running no sweep ticks while it waits,
  so a wedged connection must cost one timeout for the whole cohort rather than
  one per queued write.
  """

  alias Ingress.Nats.Publisher.Wire

  @type token :: term()
  @type request :: {token(), String.t(), iodata(), keyword()}
  @type result :: {token(), :ok | {:error, term()}}

  @spec start(pos_integer()) :: [pid()]
  def start(size) when is_integer(size) and size > 0 do
    for _ <- 1..size, do: spawn_link(&worker/0)
  end

  @spec publish([pid()], GenServer.server(), [request()], pos_integer()) :: [result()]
  def publish(_workers, _connection, [], _timeout), do: []

  def publish(workers, connection, requests, timeout) do
    assignments = assign(requests, workers)
    reference = make_ref()

    Enum.each(assignments, fn {worker, lane} ->
      send(worker, {:publish, self(), reference, connection, lane, timeout})
    end)

    collect(reference, length(assignments), [])
  end

  @spec stop([pid()]) :: :ok
  def stop(workers) do
    Enum.each(workers, &send(&1, :stop))
    :ok
  end

  defp assign(requests, workers) do
    lane_count = min(length(requests), length(workers))

    requests
    |> Enum.with_index()
    |> Enum.reduce(%{}, fn {request, index}, lanes ->
      Map.update(lanes, rem(index, lane_count), [request], &[request | &1])
    end)
    |> Enum.sort_by(&elem(&1, 0))
    |> Enum.map(fn {index, lane} -> {Enum.at(workers, index), Enum.reverse(lane)} end)
  end

  defp collect(_reference, 0, results), do: results

  defp collect(reference, remaining, results) do
    receive do
      {:published, ^reference, lane_results} ->
        collect(reference, remaining - 1, lane_results ++ results)
    end
  end

  defp worker do
    receive do
      {:publish, owner, reference, connection, requests, timeout} ->
        send(owner, {:published, reference, publish_lane(connection, requests, timeout)})
        worker()

      :stop ->
        :ok
    end
  end

  defp publish_lane(connection, requests, timeout) do
    {results, _open} =
      Enum.map_reduce(requests, true, fn {token, subject, payload, opts}, open ->
        result = lane_publish(open, connection, subject, payload, opts, timeout)
        {{token, result}, open and result == :ok}
      end)

    results
  end

  # Once one write on this lane has failed, the connection is down or wedged.
  # Fail the remainder closed instead of paying another full call timeout each:
  # the caller is blocked on all lanes, so a lane holding N queued writes would
  # otherwise turn one stalled socket into N × timeout of collector downtime.
  defp lane_publish(false, _connection, _subject, _payload, _opts, _timeout),
    do: {:error, :not_connected}

  defp lane_publish(true, connection, subject, payload, opts, timeout),
    do: Wire.safe_pub(connection, {subject, payload, opts}, timeout)
end
