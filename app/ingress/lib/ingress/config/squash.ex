defmodule Ingress.Config.Squash do
  @moduledoc """
  Chat-squash tuning (see `Ingress.Squash` and its partition pool).
  Thin accessors over application env, set once at boot by `config/runtime.exs`
  under the same `:squash_*` keys as before the split.
  """

  # How long identical non-command chat is coalesced before the cohort is
  # flushed (see Ingress.Squash).
  def window_ms,
    do: Application.get_env(:ingress, :squash_window_ms, 2_000)

  # A cohort this large flushes early instead of waiting for the window, to
  # bound the event size under a raid.
  def max_senders,
    do: Application.get_env(:ingress, :squash_max_senders, 500)

  # How often the squash sweep runs; keep it well under the window so cohorts
  # flush promptly after their window closes.
  def sweep_ms,
    do: Application.get_env(:ingress, :squash_sweep_ms, 500)

  # Independent cohort owners. Hashing by cohort key preserves ordering within
  # one flood while allowing unrelated broadcaster/text pairs to fold in
  # parallel across all online schedulers.
  def partitions,
    do: Application.get_env(:ingress, :squash_partitions, System.schedulers_online())
end
