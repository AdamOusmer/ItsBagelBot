import { jsm, js } from '@bagel/shared/server/nats';
import type { KV } from 'nats';

// Keep this boot-safe. Importing access.ts here pulls in SvelteKit's dynamic
// environment module and services.ts, creating a cycle through hooks.server.ts
// while adapter-node is still awaiting server initialization.
function isDemo(): boolean {
  return process.env.DEMO === '1';
}

export interface LaneView {
  stream: string;
  consumer: string;
  display: string;
  subject: string;
  category: string;
  ephemeral: boolean;
  orphan: boolean;
  pending: number;
  inFlight: string;
  rate: string;
  redelivered: number;
}

const sampleLanes: LaneView[] = [
  { stream: 'TWITCH_OUTGRESS', consumer: 'chat-egress', display: 'chat egress', subject: 'twitch.outgress.premium', category: 'system', ephemeral: false, orphan: false, pending: 0, inFlight: '0 / 256', rate: '18 msg/s', redelivered: 0 },
  { stream: 'TWITCH_OUTGRESS_SYSTEM', consumer: 'outgress-system_twitch_outgress_system', display: 'eventsub + live', subject: 'twitch.outgress.system', category: 'system', ephemeral: false, orphan: false, pending: 0, inFlight: '0 / 1000', rate: '0.2 msg/s', redelivered: 0 },
  { stream: 'BAGEL_DATA', consumer: 'projection-users', display: 'users projection', subject: 'bagel.data.users.>', category: 'projection', ephemeral: false, orphan: false, pending: 3, inFlight: '1', rate: '2.4 msg/s', redelivered: 0 },
  { stream: 'BAGEL_DATA', consumer: 'cache-invalidate-7f3a', display: 'ephemeral', subject: 'bagel.data.invalidate', category: 'ephemeral', ephemeral: true, orphan: true, pending: 0, inFlight: '0', rate: '—', redelivered: 0 }
];

export interface LanesResult {
  lanes: LaneView[];
  degraded: boolean;
  notice: string;
}

export interface LaneMutationResult {
  ok: boolean;
  notice?: string;
  error?: string;
}

const LANE_BUCKET = 'admin_lanes';
const LANE_STREAM = `KV_${LANE_BUCKET}`;
const NATS_REPLICAS = 3;
let kvStore: KV | null = null;

async function getKV(): Promise<KV> {
  if (kvStore) return kvStore;
  const client = await js();
  const manager = await jsm();
  try {
    const info = await manager.streams.info(LANE_STREAM);
    if (info.config.num_replicas !== NATS_REPLICAS) {
      await manager.streams.update(LANE_STREAM, {
        ...info.config,
        num_replicas: NATS_REPLICAS
      });
    }
    kvStore = await client.views.kv(LANE_BUCKET, { history: 1, replicas: NATS_REPLICAS });
  } catch (err: any) {
    if (err.code === '404' || err.message?.includes('not found')) {
      kvStore = await client.views.kv(LANE_BUCKET, {
        history: 1,
        replicas: NATS_REPLICAS,
        description: 'admin lane display aliases'
      });
    } else {
      throw err;
    }
  }
  return kvStore;
}

// Best-effort boot reconciliation; request-time callers retry through getKV if
// the hub did not yet have quorum while this process was starting.
export async function ensureLaneStoreHA(): Promise<void> {
  await getKV();
}

function laneKey(stream: string, consumer: string) {
  return `${stream}\x00${consumer}`;
}

function laneAliasKey(stream: string, consumer: string) {
  return `${stream}.${consumer}`;
}

interface LaneSample {
  delivered: number;
  at: number;
}

const prevSamples = new Map<string, LaneSample>();
let currentLanes: LaneView[] = [];
let lastError = '';
let samplerTimer: ReturnType<typeof setInterval> | null = null;
let sampling: Promise<void> | null = null;
let lastLoadAt = 0;

const SAMPLE_INTERVAL_MS = 5_000;
const SAMPLE_IDLE_MS = 60_000;
const ALIAS_CACHE_TTL_MS = 30_000;

let aliasCache = new Map<string, string>();
let aliasCacheExpires = 0;

function subjectToken(subject: string) {
  return subject.replace(/[.*>]/g, '_');
}

function laneGroup(name: string, filter: string, ephemeral: boolean) {
  if (ephemeral) return '';
  if (filter) {
    const token = subjectToken(filter);
    if (name.endsWith(`_${token}`)) {
      return name.slice(0, -token.length - 1);
    }
  }
  return name;
}

function laneCategory(stream: string, ephemeral: boolean) {
  if (ephemeral) return 'ephemeral';
  // Both outgress streams are the egress/control lanes: TWITCH_OUTGRESS (chat)
  // and TWITCH_OUTGRESS_SYSTEM (EventSub enroll + stream_status).
  if (stream.startsWith('TWITCH_OUTGRESS')) return 'system';
  return 'projection';
}

function categoryRank(category: string) {
  switch (category) {
    case 'system': return 0;
    case 'projection': return 1;
    default: return 2;
  }
}

function displayName(alias: string | undefined, group: string, consumer: string, ephemeral: boolean) {
  if (alias) return alias;
  if (ephemeral) return 'ephemeral';
  if (group) return group;
  return consumer;
}

function inFlightText(ackPending: number, maxAckPend: number) {
  if (maxAckPend > 0) return `${ackPending} / ${maxAckPend}`;
  return `${ackPending}`;
}

function rateText(rate: number, hasRate: boolean) {
  if (!hasRate) return '—';
  if (rate === 0) return '0 msg/s';
  if (rate < 10) return `${rate.toFixed(1)} msg/s`;
  return `${Math.round(rate)} msg/s`;
}

function markAliasesDirty() {
  aliasCacheExpires = 0;
}

async function loadAliases(): Promise<Map<string, string>> {
  if (aliasCacheExpires > Date.now()) return aliasCache;

  const kv = await getKV();
  const aliases = new Map<string, string>();
  try {
    // Collect keys first, then fetch values in parallel (chunked so a large
    // alias set can't fan out unbounded). The old per-key serial `await kv.get`
    // paid one KV round-trip per alias — 100 aliases = 100 sequential hops.
    const keys: string[] = [];
    const keysIter = await kv.keys();
    for await (const k of keysIter) keys.push(k);
    const CHUNK = 50;
    for (let i = 0; i < keys.length; i += CHUNK) {
      const chunk = keys.slice(i, i + CHUNK);
      const entries = await Promise.all(chunk.map((k) => kv.get(k)));
      entries.forEach((e, j) => {
        if (e) aliases.set(chunk[j], e.string());
      });
    }
  } catch (err: any) {
    if (err.code !== '404') console.warn('lane alias fetch error:', err);
  }

  aliasCache = aliases;
  aliasCacheExpires = Date.now() + ALIAS_CACHE_TTL_MS;
  return aliasCache;
}

async function collectLanes() {
  if (sampling) return sampling;
  sampling = collectLanesOnce().finally(() => {
    sampling = null;
  });
  return sampling;
}

async function collectLanesOnce() {
  try {
    const manager = await jsm();
    const aliases = await loadAliases();

    const now = Date.now();
    const rows: any[] = [];
    const seen = new Set<string>();
    let streamsSeen = 0;

    const streams = await manager.streams.list().next();
    for (const stream of streams) {
      streamsSeen++;
      const streamName = stream.config.name;
      
      const consumers = await manager.consumers.list(streamName).next();
      for (const ci of consumers) {
        const key = laneKey(streamName, ci.name);
        seen.add(key);

        const filter = ci.config.filter_subject || '';
        const ephemeral = !ci.config.durable_name;
        
        let rate = 0;
        let hasRate = false;

        const prev = prevSamples.get(key);
        if (prev) {
          const secs = (now - prev.at) / 1000;
          if (secs > 0) {
            let delta = ci.delivered.consumer_seq - prev.delivered;
            if (delta < 0) delta = 0;
            rate = delta / secs;
            hasRate = true;
          }
        }
        prevSamples.set(key, { delivered: ci.delivered.consumer_seq, at: now });

        rows.push({
          stream: streamName,
          consumer: ci.name,
          filter,
          ephemeral,
          orphan: !ci.push_bound,
          category: laneCategory(streamName, ephemeral),
          group: laneGroup(ci.name, filter, ephemeral),
          pending: ci.num_pending,
          ackPending: ci.num_ack_pending,
          maxAckPend: ci.config.max_ack_pending || 0,
          redelivered: ci.num_redelivered,
          rate,
          hasRate
        });
      }
    }

    if (streamsSeen === 0) {
      lastError = "JetStream API unreachable: no streams returned (broker unreachable or account lacks $JS.API access)";
      return;
    }

    for (const key of prevSamples.keys()) {
      if (!seen.has(key)) prevSamples.delete(key);
    }

    rows.sort((a, b) => {
      let c = categoryRank(a.category) - categoryRank(b.category);
      if (c !== 0) return c;
      c = a.stream.localeCompare(b.stream);
      if (c !== 0) return c;
      c = a.filter.localeCompare(b.filter);
      if (c !== 0) return c;
      return a.consumer.localeCompare(b.consumer);
    });

    currentLanes = rows.map(r => ({
      stream: r.stream,
      consumer: r.consumer,
      display: displayName(aliases.get(laneAliasKey(r.stream, r.consumer)), r.group, r.consumer, r.ephemeral),
      subject: r.filter,
      category: r.category,
      ephemeral: r.ephemeral,
      orphan: r.orphan,
      pending: r.pending,
      inFlight: inFlightText(r.ackPending, r.maxAckPend),
      rate: rateText(r.rate, r.hasRate),
      redelivered: r.redelivered
    }));
    lastError = '';
  } catch (err: any) {
    lastError = err.message || String(err);
  }
}

function ensureSampler() {
  lastLoadAt = Date.now();
  if (samplerTimer) return;
  samplerTimer = setInterval(() => {
    if (Date.now() - lastLoadAt > SAMPLE_IDLE_MS) {
      if (samplerTimer) clearInterval(samplerTimer);
      samplerTimer = null;
      return;
    }
    collectLanes();
  }, SAMPLE_INTERVAL_MS);
  collectLanes();
}

export async function loadLanes(): Promise<LanesResult> {
  if (isDemo()) {
    return { lanes: sampleLanes, degraded: false, notice: '' };
  }
  ensureSampler();
  if (lastError) {
    return {
      lanes: currentLanes,
      degraded: true,
      notice: 'Lane telemetry error: ' + lastError
    };
  }
  return { lanes: currentLanes, degraded: false, notice: '' };
}

export async function laneAlias(stream: string, consumer: string, alias: string): Promise<LaneMutationResult> {
  try {
    const kv = await getKV();
    const key = laneAliasKey(stream, consumer);
    if (!alias) {
      await kv.delete(key);
      markAliasesDirty();
      collectLanes(); // sample immediately
      return { ok: true, notice: 'alias cleared' };
    }
    await kv.put(key, new TextEncoder().encode(alias.slice(0, 48)));
    markAliasesDirty();
    collectLanes(); // sample immediately
    return { ok: true, notice: 'renamed to ' + alias };
  } catch (err: any) {
    return { ok: false, error: 'rename failed: ' + err.message };
  }
}

export async function laneDurable(stream: string, consumer: string): Promise<LaneMutationResult> {
  try {
    const manager = await jsm();
    const info = await manager.consumers.info(stream, consumer);
    if (info.config.durable_name) {
      return { ok: false, error: 'lane is already durable' };
    }
    const name = "adminperm_" + subjectToken(info.config.filter_subject || '');
    await manager.consumers.add(stream, {
      ...info.config,
      durable_name: name,
      description: "operator-pinned permanent lane (admin)"
    });
    collectLanes();
    return { ok: true, notice: `created permanent lane ${name}` };
  } catch (err: any) {
    return { ok: false, error: 'make-permanent failed: ' + err.message };
  }
}

export async function laneDelete(stream: string, consumer: string): Promise<LaneMutationResult> {
  try {
    const manager = await jsm();
    const info = await manager.consumers.info(stream, consumer);
    if (info.push_bound) {
      return { ok: false, error: 'refused: lane is bound to a running consumer, not an orphan' };
    }
    await manager.consumers.delete(stream, consumer);
    const kv = await getKV();
    await kv.delete(laneAliasKey(stream, consumer)).catch(() => {});
    markAliasesDirty();
    collectLanes();
    return { ok: true, notice: `deleted orphan lane ${consumer}` };
  } catch (err: any) {
    return { ok: false, error: 'delete failed: ' + err.message };
  }
}
