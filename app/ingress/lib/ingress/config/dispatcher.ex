defmodule Ingress.Config.Dispatcher do
  @moduledoc """
  Dispatcher pool tuning (see `Ingress.Dispatcher` and its worker supervisor).
  Thin accessors over application env, set once at boot by `config/runtime.exs`
  under the same `:dispatcher_*` keys as before the split.
  """

  def max_running,
    do: Application.get_env(:ingress, :dispatcher_max_running, 512)

  def max_queue,
    do: Application.get_env(:ingress, :dispatcher_max_queue, 20_000)

  # Caps how much of the pod-wide dispatcher budget (max_running + max_queue) a
  # single broadcaster can occupy at once, so a hot/raiding channel can't starve
  # every other broadcaster sharing the pod's shared worker pool.
  def max_per_broadcaster,
    do: Application.get_env(:ingress, :dispatcher_max_per_broadcaster, 2_048)

  # How often dead (zeroed) per-broadcaster counters are swept from the
  # dispatcher's ETS table, so a long-lived pod doesn't accumulate one entry per
  # distinct broadcaster ever seen.
  def broadcaster_sweep_ms,
    do: Application.get_env(:ingress, :dispatcher_broadcaster_sweep_ms, 60_000)

  # Workers fold completion bookkeeping in small local batches. Four keeps the
  # maximum unreported share below the default per-broadcaster admission cap
  # even when all 512 workers are active.
  def completion_batch_size,
    do: Application.get_env(:ingress, :dispatcher_completion_batch_size, 4)

  # Flush partial completion batches promptly when traffic goes idle.
  def completion_flush_ms,
    do: Application.get_env(:ingress, :dispatcher_completion_flush_ms, 25)
end
