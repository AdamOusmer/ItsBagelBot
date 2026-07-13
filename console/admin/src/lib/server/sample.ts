// Static fallback data so the panel renders under DEMO=1 or when an RPC
// responder is briefly unreachable. Nothing here is live; it mirrors the wire
// shapes only so the screens have something to paint.
import type { ShardSnapshot, UserStats } from '@bagel/shared';
import type { AdminUserWire, NotificationWire } from './services';

export const sampleStats: UserStats = {
  total_users: 1842,
  active_users: 312,
  premium_users: 87,
  vip_users: 12,
  paid_users: 75
};

export const sampleSnapshot: ShardSnapshot = {
  generated_at: new Date().toISOString(),
  reporter: 'ingress@10.42.0.11',
  nodes: ['ingress@10.42.0.11', 'ingress@10.42.0.12'],
  shard_count: 4,
  conduit_manager: { state: 'leader', node: 'ingress@10.42.0.11', conduit_id: 'cd_3f9a' },
  shards: [
    { shard_id: 0, state: 'connected', node: 'ingress@10.42.0.11', host: 'node1', session_id: 'sess_a1', bound: true, keepalive_ms: 30000, attempts: 0, load: 18 },
    { shard_id: 1, state: 'connected', node: 'ingress@10.42.0.11', host: 'node1', session_id: 'sess_b2', bound: true, keepalive_ms: 30000, attempts: 0, load: 240 },
    { shard_id: 2, state: 'connected', node: 'ingress@10.42.0.12', host: 'node2', session_id: 'sess_c3', bound: true, keepalive_ms: 30000, attempts: 1, load: 4200 },
    { shard_id: 3, state: 'reconnecting', node: 'ingress@10.42.0.12', host: 'node2', bound: false, handshake_in_flight: true, keepalive_ms: 0, attempts: 3, load: 0 }
  ],
  desired_count: 4,
  target: 4,
  min_shards: 2,
  autoscale: false,
  capacity: {
    benchmark: 'cached_chat_full_path_in_vm_puback',
    nats_benchmark: 'live_direct_hub_puback',
    load_window_seconds: 60,
    target_utilization_pct: 75,
    pod_rated_eps: 140_000,
    pod_target_eps: 105_000,
    fleet_nodes: 2,
    fleet_rated_eps: 280_000,
    fleet_target_eps: 210_000,
    nats_rated_eps: 86_000,
    nats_target_eps: 64_500,
    effective_rated_eps: 86_000,
    effective_target_eps: 64_500,
    bottleneck: 'nats',
    websocket_rated_eps: 12_500,
    websocket_target_eps: 9_375
  }
};

export const sampleUsers: AdminUserWire[] = [
  { id: 44322190, username: 'itsmavey', is_active: true, status: 'vip', banned: false, creator_code: 'MAVEY10', updated_at: new Date().toISOString() },
  { id: 81002934, username: 'ferret_king', is_active: true, status: 'paid', banned: false, updated_at: new Date().toISOString() },
  { id: 23910044, username: 'bagel_enjoyer', is_active: true, status: 'free', banned: false, updated_at: new Date().toISOString() },
  { id: 70113355, username: 'kettle', is_active: false, status: 'free', banned: true, updated_at: new Date().toISOString() },
  { id: 99884412, username: 'loudguy99', is_active: true, status: 'paid', banned: false, updated_at: new Date().toISOString() }
];

export const sampleNotifications: NotificationWire[] = [
  {
    id: 3,
    scope: 'broadcast',
    title: 'Scheduled maintenance tonight',
    body: 'The bot will restart briefly around midnight UTC. Commands may pause for a few seconds.',
    level: 'warning',
    created_by_login: 'itsmavey',
    created_at: new Date(Date.now() - 2 * 3600e3).toISOString(),
    read: false
  },
  {
    id: 2,
    scope: 'direct',
    title: 'Welcome aboard',
    body: "Thanks for joining ItsBagelBot — let us know if you run into anything.",
    level: 'info',
    target_user_id: 81002934,
    created_by_login: 'itsmavey',
    created_at: new Date(Date.now() - 26 * 3600e3).toISOString(),
    read: true
  }
];
