defmodule Ingress.LoadCounter do
  @moduledoc """
  A constant-time aggregate load counter for a rolling window of seconds.
  Instead of recording individual event timestamps, this tracks load in
  per-second buckets. The hot path for same-second increments is O(1).

  Timestamps are raw `System.monotonic_time(:millisecond)` values, which on
  the BEAM are negative. The counter carries no epoch of its own: it adopts
  the first timestamp it is given, so it works for any monotonic clock
  regardless of sign or origin.
  """

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

  @doc """
  Initializes a new load counter. `window_seconds` defaults to 60.
  """
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

  @doc """
  Increments the load counter for the given monotonic time in milliseconds.
  Returns the updated counter.
  """
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

  @doc """
  Returns the total load in the rolling window ending at `monotonic_ms`,
  along with the updated counter state (pruned of expired buckets).
  """
  @spec value(t(), integer()) :: {non_neg_integer(), t()}
  def value(counter, monotonic_ms) do
    sec = Integer.floor_div(monotonic_ms, 1000)
    counter = roll_to(counter, sec)
    {counter.completed_total + counter.current_count, counter}
  end

  # First timestamp ever seen: adopt it as the epoch. The struct default
  # cannot pre-fill one — BEAM monotonic time is negative, so any fixed
  # default (e.g. 0) sits permanently "in the future", every real timestamp
  # reads as a clock regression, and the window never expires. This clause
  # must stay above the regression guard: integers compare below `nil` in
  # Erlang term order, so `target_sec <= nil` is always true.
  defp roll_to(%{current_second: nil} = counter, target_sec) do
    %{counter | current_second: target_sec}
  end

  defp roll_to(counter, target_sec) when target_sec <= counter.current_second do
    # Time went backwards or didn't change (clock drift/same second).
    # Ignore the regression for bucketing purposes and just prune based on it.
    prune(counter, counter.current_second)
  end

  defp roll_to(counter, target_sec) do
    gap = target_sec - counter.current_second

    counter =
      if gap >= counter.window_seconds do
        # Fast path for long idle periods: everything expired
        %{
          counter
          | completed_buckets: [],
            completed_total: 0
        }
      else
        # Push the old current bucket onto the completed queue if it had events
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

    # The list is ordered newest to oldest, so we can stop searching once we hit
    # a bucket that is older than the cutoff.
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
    # Everything remaining in the list is expired. Sum it up.
    total_dropped = Enum.reduce(expired, dropped, fn {_, c}, acc -> acc + c end)
    {Enum.reverse(acc), total_dropped}
  end
end
