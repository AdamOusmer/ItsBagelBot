# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.LoadCounter do
  defstruct window_seconds: 60,
            current_second: nil,
            current_count: 0,
            completed_buckets: [],
            completed_total: 0

  @type t :: %__MODULE__{
          window_seconds: pos_integer(),
          current_second: integer() | nil,
          current_count: non_neg_integer(),
          completed_buckets: [{integer(), non_neg_integer()}],
          completed_total: non_neg_integer()
        }

  @spec new(pos_integer()) :: t()
  def new(window_seconds \\ 60) do
    %__MODULE__{
      window_seconds: window_seconds,
      current_second: nil,
      current_count: 0,
      completed_buckets: [],
      completed_total: 0
    }
  end

  @spec increment(t(), integer()) :: t()
  def increment(counter, monotonic_ms) do
    sec = Integer.floor_div(monotonic_ms, 1000)

    if sec == counter.current_second do
      %{counter | current_count: counter.current_count + 1}
    else
      counter
      |> roll_to(sec)
      |> Map.update!(:current_count, &(&1 + 1))
    end
  end

  @spec value(t(), integer()) :: {non_neg_integer(), t()}
  def value(counter, monotonic_ms) do
    sec = Integer.floor_div(monotonic_ms, 1000)
    counter = roll_to(counter, sec)
    {counter.completed_total + counter.current_count, counter}
  end

  # Must stay above the regression guard: integers sort below nil.
  defp roll_to(%{current_second: nil} = counter, target_sec) do
    %{counter | current_second: target_sec}
  end

  defp roll_to(counter, target_sec) when target_sec <= counter.current_second do
    prune(counter, counter.current_second)
  end

  defp roll_to(counter, target_sec) do
    gap = target_sec - counter.current_second

    counter =
      if gap >= counter.window_seconds do
        %{
          counter
          | completed_buckets: [],
            completed_total: 0
        }
      else
        {buckets, total} =
          if counter.current_count > 0 do
            {
              [{counter.current_second, counter.current_count} | counter.completed_buckets],
              counter.completed_total + counter.current_count
            }
          else
            {counter.completed_buckets, counter.completed_total}
          end

        %{counter | completed_buckets: buckets, completed_total: total}
      end

    counter
    |> Map.put(:current_second, target_sec)
    |> Map.put(:current_count, 0)
    |> prune(target_sec)
  end

  defp prune(counter, target_sec) do
    cutoff_sec = target_sec - counter.window_seconds

    {kept, dropped_count} = do_prune(counter.completed_buckets, [], 0, cutoff_sec)

    %{
      counter
      | completed_buckets: kept,
        completed_total: counter.completed_total - dropped_count
    }
  end

  defp do_prune([{sec, _count} = bucket | rest], acc, dropped, cutoff) when sec > cutoff do
    do_prune(rest, [bucket | acc], dropped, cutoff)
  end

  defp do_prune(expired, acc, dropped, _cutoff) do
    total_dropped = Enum.reduce(expired, dropped, fn {_, c}, acc -> acc + c end)
    {Enum.reverse(acc), total_dropped}
  end
end
