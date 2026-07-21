defmodule Ingress.Config.Admin do
  @moduledoc """
  Admin/control RPC subjects (consumed only by `Ingress.Application`'s
  consumer children). Thin accessors over application env, set once at boot by
  `config/runtime.exs` under the same keys as before the split.
  """

  def admin_subject, do: Application.fetch_env!(:ingress, :admin_subject)

  # Subject for manual shard-count scaling: body {"count": N}.
  def scale_subject, do: Application.fetch_env!(:ingress, :scale_subject)

  # Subject for toggling the load-based autoscaler: body {"enabled": true|false}.
  def autoscale_subject, do: Application.fetch_env!(:ingress, :autoscale_subject)

  # Subject for live conduit id query: body {}, replies {"conduit_id": "<uuid>"}.
  def conduit_subject, do: Application.fetch_env!(:ingress, :conduit_subject)

  # Side-effect-free admin RPC latency probe.
  def rpc_health_subject, do: Application.fetch_env!(:ingress, :rpc_health_subject)
end
