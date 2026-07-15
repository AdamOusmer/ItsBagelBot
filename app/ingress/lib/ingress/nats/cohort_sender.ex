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
  """

  @type token :: term()
  @type request :: {token(), String.t(), iodata(), keyword()}
  @type result :: {token(), :ok | {:error, term()}}

  @spec start(pos_integer()) :: [pid()]
  def start(size) when is_integer(size) and size > 0 do
    for _ <- 1..size, do: spawn_link(&worker/0)
  end

  @spec publish([pid()], GenServer.server(), [request()]) :: [result()]
  def publish(_workers, _connection, []), do: []

  def publish(workers, connection, requests) do
    assignments = assign(requests, workers)
    reference = make_ref()

    Enum.each(assignments, fn {worker, lane} ->
      send(worker, {:publish, self(), reference, connection, lane})
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
      {:publish, owner, reference, connection, requests} ->
        results =
          Enum.map(requests, fn {token, subject, payload, opts} ->
            {token, safe_publish(connection, subject, payload, opts)}
          end)

        send(owner, {:published, reference, results})
        worker()

      :stop ->
        :ok
    end
  end

  defp safe_publish(connection, subject, payload, opts) do
    Gnat.pub(connection, subject, payload, opts)
  catch
    :exit, _reason -> {:error, :not_connected}
  end
end
