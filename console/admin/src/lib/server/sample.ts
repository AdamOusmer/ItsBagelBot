// Static fallback data so the panel renders under DEMO=1 or when an RPC
// responder is briefly unreachable. Nothing here is live; it mirrors the wire
// shapes only so the screens have something to paint.
import type { ShardSnapshot, UserStats } from '@bagel/shared';
import type { AdminUserWire, AuditEntry, EnrollmentWire, NotificationWire, ServiceHealth } from './services';

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
    nats_rated_eps: 123_000,
    nats_target_eps: 92_250,
    effective_rated_eps: 123_000,
    effective_target_eps: 92_250,
    bottleneck: 'nats',
    websocket_rated_eps: 12_500,
    websocket_target_eps: 9_375
  }
};

// Deterministic signup curve (gentle growth + weekend bumps), so the demo
// histogram looks plausible without pretending to be live.
export function demoEnrollment(days = 30): EnrollmentWire {
  return {
    days: Array.from({ length: days }, (_, i) => {
      const d = new Date(Date.now() - (days - 1 - i) * 864e5);
      const wave = Math.round(6 + 4 * Math.sin(i / 4) + (d.getUTCDay() % 6 === 0 ? 5 : 0));
      return { date: d.toISOString().slice(0, 10), count: Math.max(0, wave + (i % 7 === 3 ? 3 : 0)) };
    }),
    stats: sampleStats
  };
}

export const sampleEnrollment: EnrollmentWire = demoEnrollment(30);

export const sampleUsers: AdminUserWire[] = [
  { id: 44322190, username: 'itsmavey', is_active: true, status: 'vip', banned: false, creator_code: 'MAVEY10', created_at: new Date(Date.now() - 400 * 864e5).toISOString(), updated_at: new Date().toISOString() },
  { id: 81002934, username: 'ferret_king', is_active: true, status: 'paid', banned: false, subscription_expires_at: new Date(Date.now() + 21 * 864e5).toISOString(), subscription_source: 'tebex', created_at: new Date(Date.now() - 90 * 864e5).toISOString(), updated_at: new Date().toISOString() },
  { id: 23910044, username: 'bagel_enjoyer', is_active: true, status: 'free', banned: false, created_at: new Date(Date.now() - 30 * 864e5).toISOString(), updated_at: new Date().toISOString() },
  { id: 70113355, username: 'kettle', is_active: false, status: 'free', banned: true, created_at: new Date(Date.now() - 200 * 864e5).toISOString(), updated_at: new Date().toISOString() },
  { id: 99884412, username: 'loudguy99', is_active: false, status: 'paid', banned: false, created_at: new Date(Date.now() - 7 * 864e5).toISOString(), updated_at: new Date().toISOString() }
];

export const sampleHealth: ServiceHealth[] = [
  { id: 'users', label: 'Users', ok: true, ms: 4 },
  { id: 'commands', label: 'Commands', ok: true, ms: 6 },
  { id: 'modules', label: 'Modules', ok: true, ms: 7 },
  { id: 'loyalty', label: 'Loyalty', ok: true, ms: 9 },
  { id: 'projector', label: 'Projector', ok: true, ms: 5 },
  { id: 'sesame', label: 'Sesame', ok: true, ms: 8 },
  { id: 'gateway', label: 'Gateway', ok: true, ms: 12 },
  { id: 'ingress', label: 'Ingress', ok: true, ms: 11 },
  { id: 'outgress', label: 'Outgress', ok: true, ms: 7 },
  { id: 'transactions', label: 'Transactions', ok: true, ms: 10 },
  { id: 'notifications', label: 'Notifications', ok: false, ms: 1500, error: 'timeout' }
];

export const sampleAudit: AuditEntry[] = [
  { id: 41, actor_id: 804932984, actor_login: 'itsmavey', action: 'set_status', target: '81002934', detail: 'status=paid end=2026-08-01', ok: true, created_at: new Date(Date.now() - 40 * 60e3).toISOString() },
  { id: 40, actor_id: 111111111, actor_login: 'an_admin', action: 'restart', target: '23910044', ok: true, created_at: new Date(Date.now() - 3 * 3600e3).toISOString() },
  { id: 39, actor_id: 804932984, actor_login: 'itsmavey', action: 'ban', target: '70113355', ok: true, created_at: new Date(Date.now() - 8 * 3600e3).toISOString() },
  { id: 38, actor_id: 111111111, actor_login: 'an_admin', action: 'db_credential_rotate', target: 'modules', detail: 'modules_svc_r1x9k2', ok: false, error: 'Doppler request failed (403)', created_at: new Date(Date.now() - 26 * 3600e3).toISOString() }
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
