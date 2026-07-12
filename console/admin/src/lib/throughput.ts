import type { IngressCapacity, ShardSnapshot } from '@bagel/shared';

const FALLBACK_POD_RATED_EPS = 140_000;
const FALLBACK_WEBSOCKET_RATED_EPS = 12_500;
const FALLBACK_NATS_RATED_EPS = 44_000;
const FALLBACK_TARGET_PCT = 75;
const FALLBACK_WINDOW_SECONDS = 60;

/**
 * Resolve capacity from the ingress snapshot. The fallback only exists for
 * mixed-version rolling deployments; ingress is the source of truth once the
 * capacity field is available.
 */
export function resolveCapacity(snapshot: ShardSnapshot): IngressCapacity {
  if (snapshot.capacity) return snapshot.capacity;

  const nodes = Math.max(snapshot.nodes.length, 1);
  const podTarget = Math.floor((FALLBACK_POD_RATED_EPS * FALLBACK_TARGET_PCT) / 100);
  const websocketTarget = Math.floor(
    (FALLBACK_WEBSOCKET_RATED_EPS * FALLBACK_TARGET_PCT) / 100
  );
  const natsTarget = Math.floor((FALLBACK_NATS_RATED_EPS * FALLBACK_TARGET_PCT) / 100);
  const fleetRated = FALLBACK_POD_RATED_EPS * nodes;
  const fleetTarget = podTarget * nodes;

  return {
    benchmark: 'cached_chat_full_path_in_vm_puback',
    nats_benchmark: 'live_direct_hub_puback',
    load_window_seconds: FALLBACK_WINDOW_SECONDS,
    target_utilization_pct: FALLBACK_TARGET_PCT,
    pod_rated_eps: FALLBACK_POD_RATED_EPS,
    pod_target_eps: podTarget,
    fleet_nodes: nodes,
    fleet_rated_eps: fleetRated,
    fleet_target_eps: fleetTarget,
    nats_rated_eps: FALLBACK_NATS_RATED_EPS,
    nats_target_eps: natsTarget,
    effective_rated_eps: Math.min(fleetRated, FALLBACK_NATS_RATED_EPS),
    effective_target_eps: Math.min(fleetTarget, natsTarget),
    bottleneck: FALLBACK_NATS_RATED_EPS <= fleetRated ? 'nats' : 'ingress_compute',
    websocket_rated_eps: FALLBACK_WEBSOCKET_RATED_EPS,
    websocket_target_eps: websocketTarget
  };
}

export function eventsPerSecond(load: number | undefined, windowSeconds: number): number {
  if (load == null) return 0;
  if (load <= 0) return 0;
  if (windowSeconds <= 0) return 0;
  return load / windowSeconds;
}

export function utilizationPct(rate: number, ratedEps: number): number {
  if (rate <= 0 || ratedEps <= 0) return 0;
  return (rate / ratedEps) * 100;
}

export function barWidth(utilization: number): number {
  return Math.min(100, Math.max(0, Math.round(utilization)));
}

export function utilizationTone(
  utilization: number,
  targetUtilization: number
): 'green' | 'warn' | 'err' {
  if (utilization >= targetUtilization) return 'err';
  if (utilization >= targetUtilization * 0.8) return 'warn';
  return 'green';
}
