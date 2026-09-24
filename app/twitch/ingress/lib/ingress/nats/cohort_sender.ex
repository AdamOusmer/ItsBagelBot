# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.CohortSender do
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

    workers = List.to_tuple(workers)

    requests
    |> Enum.with_index()
    |> Enum.reduce(%{}, fn {request, index}, lanes ->
      Map.update(lanes, rem(index, lane_count), [request], &[request | &1])
    end)
    |> Enum.sort_by(&elem(&1, 0))
    |> Enum.map(fn {index, lane} -> {elem(workers, index), Enum.reverse(lane)} end)
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

  defp lane_publish(false, _connection, _subject, _payload, _opts, _timeout),
    do: {:error, :not_connected}

  defp lane_publish(true, connection, subject, payload, opts, timeout),
    do: Wire.safe_pub(connection, {subject, payload, opts}, timeout)
end
