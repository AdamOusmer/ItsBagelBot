// Static fallback data so the panel renders under DEMO=1 or when an RPC
// responder is briefly unreachable. Nothing here is live; it mirrors the wire
// shapes only so the screens have something to paint.
import type { ShardSnapshot, UserStats } from '@bagel/shared';
import type { AdminUserWire } from './rpc';

export const sampleStats: UserStats = {
  total_users: 1842,
  active_users: 312,
  premium_users: 87,
  vip_users: 12,
  paid_users: 75
};

export const sampleSnapshot: ShardSnapshot = {
  generated_at: new Date().toISOString(),
  reporter: 'ingress@node1',
  nodes: ['node1', 'node2'],
  shard_count: 4,
  conduit_manager: { state: 'leader', node: 'node1', conduit_id: 'cd_3f9a' },
  shards: [
    { shard_id: 0, state: 'connected', node: 'node1', session_id: 'sess_a1', bound: true, keepalive_ms: 30000, attempts: 0, load: 0.42 },
    { shard_id: 1, state: 'connected', node: 'node1', session_id: 'sess_b2', bound: true, keepalive_ms: 30000, attempts: 0, load: 0.61 },
    { shard_id: 2, state: 'connected', node: 'node2', session_id: 'sess_c3', bound: true, keepalive_ms: 30000, attempts: 1, load: 0.29 },
    { shard_id: 3, state: 'reconnecting', node: 'node2', bound: false, handshake_in_flight: true, keepalive_ms: 0, attempts: 3, load: 0.0 }
  ],
  desired_count: 4,
  target: 4,
  min_shards: 2,
  autoscale: false
};

export const sampleUsers: AdminUserWire[] = [
  { id: 44322190, username: 'itsmavey', is_active: true, status: 'vip', banned: false, updated_at: new Date().toISOString() },
  { id: 81002934, username: 'ferret_king', is_active: true, status: 'paid', banned: false, updated_at: new Date().toISOString() },
  { id: 23910044, username: 'bagel_enjoyer', is_active: true, status: 'free', banned: false, updated_at: new Date().toISOString() },
  { id: 70113355, username: 'kettle', is_active: false, status: 'free', banned: true, updated_at: new Date().toISOString() },
  { id: 99884412, username: 'loudguy99', is_active: true, status: 'paid', banned: false, updated_at: new Date().toISOString() }
];
