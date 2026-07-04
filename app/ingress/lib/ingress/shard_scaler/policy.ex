defmodule Ingress.ShardScaler.Policy do
  @moduledoc """
  Pure logic for autoscaler decisions.
  """

  # Notification events per @load_window_ms per shard above which we add a shard.
  @scale_up_threshold 50
  # Notification events per @load_window_ms per shard below which we consider
  # removing a shard (requires @scale_down_ticks consecutive ticks).
  @scale_down_threshold 10
  # Consecutive low-load ticks required before scaling down (hysteresis).
  @scale_down_ticks 3

  @doc """
  Evaluates load and determines the new target and hysteresis state.

  Returns `{new_target, new_low_ticks, action}` where action is `:up`, `:down`, or `:hold`.
  """
  def evaluate(sample, current_target, current_low_ticks, min_shards, max_shards) do
    if sample.responsive_count == 0 do
      {current_target, 0, :hold}
    else
      avg = sample.avg_load

      cond do
        avg > @scale_up_threshold ->
          new_target = min(current_target + 1, max_shards)
          {new_target, 0, :up}

        avg < @scale_down_threshold ->
          if sample.missing_count > 0 do
            # Incomplete sample: reset scale down streak and hold.
            {current_target, 0, :hold}
          else
            ticks = current_low_ticks + 1

            if ticks >= @scale_down_ticks do
              new_target = max(current_target - 1, min_shards)
              {new_target, 0, :down}
            else
              {current_target, ticks, :hold}
            end
          end

        true ->
          {current_target, 0, :hold}
      end
    end
  end
end
