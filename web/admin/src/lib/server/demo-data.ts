// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import type { ShardSnapshot, UserStats } from '@bagel/kit';
import type {
  TrialSnapshot,
  AdminAcct,
  AdminUserWire,
  AuditEntry,
  EnrollmentWire,
  NotificationWire,
  ServiceHealth
} from './services';
import type { AdminIdentity } from './access';
import type { FeedEvent } from './feed';
import type { LaneView } from './lanes';
import type { DbCredentialStatus, ScopeReport, SecretServiceId } from './secrets';
import type { DeployApi } from './deploys';
import type { DeployListener, DeployRunId, DeployRuns } from './services';
import { RpcError } from '@bagel/kit/server/nats';
import {
  STAGES_FOR,
  TERMINAL_RUN_STATES,
  type DeployItem,
  type DeployPlan,
  type DeployRun,
  type DeployRunSummary,
  type DeployStage,
  type PodPhase,
  type RunRequest,
  type StageId,
  type StageState,
  type StartRequest
} from '$lib/deploys/types';

if (!dev) throw new Error('ADMIN_DEV_FIXTURE_INCLUDED_IN_PRODUCTION');

export function demoAdminIdentity(): AdminIdentity {
  return {
    id: 'demo-admin',
    login: 'itsmavey',
    display_name: 'Mavey',
    role: 'owner'
  };
}

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
  max_shards: 11,
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
    websocket_rated_eps: 16_000,
    websocket_target_eps: 12_000,
    websocket_autoscale_max_shards: 11
  },
  trial_loads: { '38871579': 3, '522478761': 190, '128002336': 410, '40934651': 0 },
  trial_sockets: [
    { slot: 0, node: 'ingress@10.42.0.11', state: 'connected', channels: 2, load: 413 },
    { slot: 1, node: 'ingress@10.42.0.12', state: 'connected', channels: 1, load: 190 },
    { slot: 2, node: 'ingress@10.42.0.12', state: 'connecting', channels: 1, load: 0 }
  ]
};

export const sampleTrials: TrialSnapshot = {
  version: 1,
  active_count: 4,
  max_channels: 30,
  socket_target: 3,
  trials: [
    { broadcaster_id: '38871579', display_name: 'Feinberg', state: 'receiving', enabled: true, slot: 0, received: 2 },
    { broadcaster_id: '128002336', display_name: 's0mcs', state: 'receiving', enabled: true, slot: 0, received: 911 },
    { broadcaster_id: '522478761', display_name: 'hackingnoisess', state: 'receiving', enabled: true, slot: 1, received: 617 },
    { broadcaster_id: '40934651', display_name: 'Ludwig', state: 'pending', enabled: true, slot: 2, received: 0 }
  ]
};

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
  { id: 'gossip', label: 'Gossip', ok: true, ms: 12 },
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
    body: "Thanks for joining ItsBagelBot, let us know if you run into anything.",
    level: 'info',
    target_user_id: 81002934,
    created_by_login: 'itsmavey',
    created_at: new Date(Date.now() - 26 * 3600e3).toISOString(),
    read: true
  }
];

export const sampleLanes: LaneView[] = [
  { stream: 'TWITCH_OUTGRESS', consumer: 'chat-egress', display: 'chat egress', subject: 'twitch.outgress.premium', category: 'system', ephemeral: false, orphan: false, pending: 0, inFlight: '0 / 256', rate: '18 msg/s', redelivered: 0 },
  { stream: 'TWITCH_OUTGRESS_SYSTEM', consumer: 'outgress-system_twitch_outgress_system', display: 'eventsub + live', subject: 'twitch.outgress.system', category: 'system', ephemeral: false, orphan: false, pending: 0, inFlight: '0 / 1000', rate: '0.2 msg/s', redelivered: 0 },
  { stream: 'BAGEL_DATA', consumer: 'projection-users', display: 'users projection', subject: 'bagel.data.users.>', category: 'projection', ephemeral: false, orphan: false, pending: 3, inFlight: '1', rate: '2.4 msg/s', redelivered: 0 },
  { stream: 'BAGEL_DATA', consumer: 'cache-invalidate-7f3a', display: 'ephemeral', subject: 'bagel.data.invalidate', category: 'ephemeral', ephemeral: true, orphan: true, pending: 0, inFlight: '0', rate: '-', redelivered: 0 }
];

export function demoAuditPageEntries(): AuditEntry[] {
  const now = Date.now();
  return [
    { id: 3, actor_id: 804932984, actor_login: 'itsmavey', action: 'dashboard:command:update', target: '111111111', detail: '!uptime', ok: true, created_at: new Date(now - 60_000).toISOString() },
    { id: 2, actor_id: 804932984, actor_login: 'itsmavey', action: 'impersonate', target: '111111111', detail: '', ok: true, created_at: new Date(now - 3_600_000).toISOString() },
    { id: 1, actor_id: 804932984, actor_login: 'itsmavey', action: 'delete', target: '333333333', detail: '', ok: false, error: 'user not found', created_at: new Date(now - 7_200_000).toISOString() }
  ];
}

export function demoStaff(): AdminAcct[] {
  return [
    { id: 804932984, login: 'itsmavey', display_name: 'itsmavey', role: 'owner', active: true, added_by: 0, created_at: new Date(Date.now() - 86400_000 * 30).toISOString() },
    { id: 111111111, login: 'an_admin', display_name: 'An Admin', role: 'admin', active: true, added_by: 804932984, created_at: new Date(Date.now() - 86400_000 * 7).toISOString() },
    { id: 222222222, login: 'a_mod', display_name: 'A Mod', role: 'moderator', active: true, added_by: 804932984, created_at: new Date(Date.now() - 86400_000 * 2).toISOString() }
  ];
}

export function demoStaffHistory(actorId: number): AuditEntry[] {
  const now = Date.now();
  return [
    { id: 2, actor_id: actorId, actor_login: 'demo', action: 'set_status', target: '111111111', detail: 'paid', ok: true, created_at: new Date(now - 3_600_000).toISOString() },
    { id: 1, actor_id: actorId, actor_login: 'demo', action: 'staff_upsert', target: '222222222', detail: 'a_mod:moderator', ok: true, created_at: new Date(now - 7_200_000).toISOString() }
  ];
}

export type DemoSecretsBundle = {
  services: DbCredentialStatus[];
  scope: ScopeReport;
};

export function demoSecretsBundle(ids: readonly SecretServiceId[]): DemoSecretsBundle {
  return {
    services: ids.map((id) => ({
      id,
      label: id.charAt(0).toUpperCase() + id.slice(1),
      project: id,
      config: 'prd',
      schema: `bagel_${id}`,
      expectedUserPrefix: `${id}_svc`,
      dbUser: `${id}_svc_r1demo00`,
      autoMigrate: 'false',
      canReadDoppler: true,
      tokenSource: 'scoped'
    })),
    scope: {
      sources: {
        users: 'scoped',
        commands: 'scoped',
        modules: 'scoped',
        transactions: 'scoped',
        notifications: 'scoped',
        'discord-data': 'scoped'
      }
    }
  };
}

const secretNotices = {
  db_credential_rotate: 'credential rotated (demo)',
  db_credential_set: 'credential set (demo)',
  db_credential_revoke: 'credential revoked (demo)'
} as const;

export function demoSecretNotice(action: keyof typeof secretNotices): string {
  return secretNotices[action];
}

export function demoFeedEvent(sequence: number, statusPrefix: string): FeedEvent {
  const tones: FeedEvent['tone'][] = ['up', 'neutral', 'down'];
  const tone = tones[sequence % tones.length];
  const state = tone === 'up' ? 'up' : tone === 'down' ? 'down' : 'keepalive';
  return {
    subject: `${statusPrefix}.shard.${sequence % 4}.${state}`,
    label: `shard.${sequence % 4}`,
    tone,
    payload: `demo event #${sequence}`,
    time: new Date().toLocaleTimeString('en-GB', { hour12: false })
  };
}

const DEMO_TICK_MS = 1500;
const DEMO_NODES = ['node1', 'node2', 'node3'];
const DEMO_SERVICES = ['users', 'gossip', 'twitch-ingress', 'console-admin'] as const;
type DemoService = (typeof DEMO_SERVICES)[number];
const DEMO_REPO = 'https://github.com/AdamOusmer/ItsBagelBot';
const DEMO_SHA = '4f1c9e2ab7d05e8c3a6b91f0d2e47c5a8b3f6e19';
const demoRuns = new Map<DeployRunId, DeployRun>();

const DEMO_JOBS = 2;
const DEMO_UNITS: Partial<Record<StageId, number>> = {
  build: DEMO_SERVICES.length * DEMO_JOBS,
  rollout: DEMO_SERVICES.length * DEMO_NODES.length
};

function demoStage(id: StageId): DeployStage {
  return { id, state: 'pending', progress: { done: 0, total: DEMO_UNITS[id] ?? 1 } };
}

function demoRunFrom(req: StartRequest, id: DeployRunId): DeployRun {
  const now = new Date().toISOString();
  const { actor_id, ...fields } = req;
  return {
    ...fields,
    id,
    seq: 1,
    actor: { id: actor_id, login: 'itsmavey' },
    state: 'running',
    stages: STAGES_FOR[req.kind].map(demoStage),
    outputs: { messaging_changed: false },
    created_at: now,
    updated_at: now
  };
}

function demoPut(run: DeployRun): DeployRun {
  demoRuns.set(run.id, run);
  return run;
}

function demoGet(req: RunRequest): DeployRun {
  const run = demoRuns.get(req.run_id);
  if (!run) throw new RpcError('run not found', 'not_found');
  return run;
}

type DemoSlot = { service: DemoService; index: number; per: number; done: number };

function slotFresh(s: DemoSlot): number {
  return Math.min(Math.max(s.done - s.index * s.per, 0), s.per);
}

function slotStarted(s: DemoSlot): boolean {
  return s.done >= s.index * s.per;
}

function slotState(s: DemoSlot): StageState {
  if (slotFresh(s) >= s.per) return 'succeeded';
  return slotStarted(s) ? 'running' : 'pending';
}

function slotPodPhase(s: DemoSlot, pod: number): PodPhase {
  const fresh = slotFresh(s);
  if (pod < fresh) return 'new';
  return slotStarted(s) && pod === fresh ? 'pending' : 'old';
}

function demoBuildItem(s: DemoSlot): DeployItem {
  return {
    key: s.service,
    label: s.service,
    state: slotState(s),
    url: `${DEMO_REPO}/actions`,
    progress: { done: slotFresh(s), total: s.per }
  };
}

function demoRolloutItem(s: DemoSlot): DeployItem {
  return {
    key: s.service,
    label: s.service,
    state: slotState(s),
    progress: { done: slotFresh(s), total: s.per },
    nodes: DEMO_NODES.map((node, pod) => ({ node, pod: `${s.service}-${pod}`, phase: slotPodPhase(s, pod) }))
  };
}

const DEMO_ITEMS: Partial<Record<StageId, { per: number; item: (s: DemoSlot) => DeployItem }>> = {
  build: { per: DEMO_JOBS, item: demoBuildItem },
  rollout: { per: DEMO_NODES.length, item: demoRolloutItem }
};

function demoStep(stage: DeployStage): void {
  const done = stage.progress.done + 1;
  stage.progress = { ...stage.progress, done };
  stage.state = done >= stage.progress.total ? 'succeeded' : 'running';
  const spec = DEMO_ITEMS[stage.id];
  if (spec) stage.items = DEMO_SERVICES.map((service, index) => spec.item({ service, index, per: spec.per, done }));
}

function demoAdvance(run: DeployRun): DeployRun {
  if (run.state !== 'running') return run;
  const next = structuredClone(run);
  const stage = next.stages.find((s) => s.state !== 'succeeded');
  if (stage) demoStep(stage);
  else next.state = 'succeeded';
  next.seq += 1;
  next.updated_at = new Date().toISOString();
  return demoPut(next);
}

function demoSummary(run: DeployRun): DeployRunSummary {
  return {
    id: run.id,
    kind: run.kind,
    version: run.version,
    target_sha: run.target_sha,
    state: run.state,
    current_stage: run.stages.find((s) => s.state !== 'succeeded')?.id,
    actor: run.actor,
    created_at: run.created_at,
    updated_at: run.updated_at
  };
}

function demoList(): DeployRuns {
  const runs = [...demoRuns.values()].reverse();
  const active = runs.find((r) => !TERMINAL_RUN_STATES.includes(r.state));
  return { runs: runs.map(demoSummary), activeRunId: active?.id ?? null };
}

function demoStart(req: StartRequest): DeployRun {
  if (demoList().activeRunId) throw new RpcError('a deploy is already running', 'conflict');
  return demoPut(demoRunFrom(req, `demo-${Date.now().toString(36)}`));
}

function demoCancel(req: RunRequest): DeployRun {
  const run = structuredClone(demoGet(req));
  run.cancel_requested = true;
  run.state = 'cancelled';
  run.seq += 1;
  return demoPut(run);
}

function demoWatch(runId: DeployRunId, listener: DeployListener): () => void {
  const tick = setInterval(() => {
    const run = demoRuns.get(runId);
    if (run) listener.run(demoAdvance(run));
  }, DEMO_TICK_MS);
  return () => clearInterval(tick);
}

function demoDeployPlan(): DeployPlan {
  return {
    live_version: 'v0.2.0-beta',
    live_sha: DEMO_SHA,
    last_tag: 'v0.2.0-beta',
    next_version: 'v0.3.0-beta',
    main_sha: DEMO_SHA,
    in_sync: true,
    commits: [{ sha: DEMO_SHA, title: 'feat(time): answer !time <place> with a lookup (#1006)', author: 'AdamOusmer', pr: 1006 }],
    prs: [
      {
        number: 1010,
        title: 'feat(dashboard): public commands page toggle',
        author: 'AdamOusmer',
        url: `${DEMO_REPO}/pull/1010`,
        head_sha: DEMO_SHA,
        draft: false,
        checks: 'success',
        codescene: 'success',
        mergeable: true,
        behind: false
      }
    ],
    releases: [
      { version: 'v0.2.0-beta', sha: DEMO_SHA, url: `${DEMO_REPO}/releases/tag/v0.2.0-beta`, published_at: '2026-09-10T18:00:00Z' }
    ],
    services: [...DEMO_SERVICES]
  };
}

demoPut({
  ...demoRunFrom({ actor_id: 'demo-admin', kind: 'release', version: 'v0.2.0-beta', target_sha: DEMO_SHA }, 'demo-v0-2-0'),
  state: 'succeeded',
  stages: STAGES_FOR.release.map((id) => {
    const stage = demoStage(id);
    return { ...stage, state: 'succeeded', progress: { ...stage.progress, done: stage.progress.total } };
  })
});

export function demoDeployApi(): DeployApi {
  const same = async (req: RunRequest) => demoGet(req);
  return {
    plan: async () => demoDeployPlan(),
    list: async () => demoList(),
    get: same,
    start: async (req) => demoStart(req),
    resume: same,
    cancel: async (req) => demoCancel(req),
    approve: same,
    watch: demoWatch
  };
}
