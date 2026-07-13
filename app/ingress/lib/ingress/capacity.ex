defmodule Ingress.Capacity do
  @moduledoc """
  Authoritative capacity values shared by the autoscaler and admin snapshot.

  The pod compute rating is the rounded-down result of the production-shaped
  two-scheduler cached-chat benchmark. It covers dispatcher admission,
  routing, squash bookkeeping, JSON encoding and async PubAck reconciliation;
  external broker storage and network latency are deliberately not claimed by
  that number. The separate NATS rating comes from the native-TLS live-cluster
  direct-hub PubAck acceptance test. Effective fleet capacity is the lower of
  aggregate pod compute and that shared NATS limit.

  WebSocket capacity is a separate constraint. Shard count controls how many
  serial socket read/decode loops Twitch can spread traffic across, while pod
  count controls the parallel end-to-end processing capacity of the cluster.
  """

  @load_window_seconds 60
  @default_pod_rated_eps 140_000
  @default_nats_rated_eps 86_000
  @default_websocket_rated_eps 12_500
  @default_target_utilization_pct 75

  def load_window_seconds, do: @load_window_seconds

  def pod_rated_eps,
    do: Application.get_env(:ingress, :capacity_pod_rated_eps, @default_pod_rated_eps)

  def websocket_rated_eps,
    do:
      Application.get_env(
        :ingress,
        :capacity_websocket_rated_eps,
        @default_websocket_rated_eps
      )

  def nats_rated_eps,
    do: Application.get_env(:ingress, :capacity_nats_rated_eps, @default_nats_rated_eps)

  def target_utilization_pct,
    do:
      Application.get_env(
        :ingress,
        :capacity_target_utilization_pct,
        @default_target_utilization_pct
      )

  def pod_target_eps, do: at_target(pod_rated_eps())
  def nats_target_eps, do: at_target(nats_rated_eps())
  def websocket_target_eps, do: at_target(websocket_rated_eps())

  @doc """
  Capacity metadata for the admin wire snapshot.

  Fleet capacity grows with live BEAM nodes (one ingress pod per node name),
  not with WebSocket shard count.
  """
  def snapshot(node_count) when is_integer(node_count) and node_count > 0 do
    fleet_rated = pod_rated_eps() * node_count
    fleet_target = pod_target_eps() * node_count
    effective_rated = min(fleet_rated, nats_rated_eps())
    effective_target = min(fleet_target, nats_target_eps())

    %{
      benchmark: "cached_chat_full_path_in_vm_puback",
      nats_benchmark: "live_direct_hub_puback",
      load_window_seconds: load_window_seconds(),
      target_utilization_pct: target_utilization_pct(),
      pod_rated_eps: pod_rated_eps(),
      pod_target_eps: pod_target_eps(),
      fleet_nodes: node_count,
      fleet_rated_eps: fleet_rated,
      fleet_target_eps: fleet_target,
      nats_rated_eps: nats_rated_eps(),
      nats_target_eps: nats_target_eps(),
      effective_rated_eps: effective_rated,
      effective_target_eps: effective_target,
      bottleneck: if(nats_rated_eps() <= fleet_rated, do: "nats", else: "ingress_compute"),
      websocket_rated_eps: websocket_rated_eps(),
      websocket_target_eps: websocket_target_eps()
    }
  end

  defp at_target(rating), do: div(rating * target_utilization_pct(), 100)
end
