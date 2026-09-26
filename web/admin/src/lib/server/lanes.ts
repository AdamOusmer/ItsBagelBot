// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { jsm, js } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { Kvm, type KV } from '@nats-io/kv';
import { dev } from '$app/environment';
import { allPages, consumerActivity, deliveryRate, formatDeliveryRate, type DeliverySample } from './lane-telemetry';

// Do not import access.ts: it pulls in services.ts, an import cycle through hooks.server.ts at boot.
const DEMO = dev && process.env.DEMO === '1';

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
  delivered?: number;
  ratePerSecond?: number | null;
  ackPending?: number;
  maxAckPending?: number;
  waiting?: number;
  mode?: 'push' | 'pull';
  ackPolicy?: string;
  connection?: 'bound' | 'waiting' | 'unbound' | 'unknown';
}

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
    kvStore = await new Kvm(client).create(LANE_BUCKET, { history: 1, replicas: NATS_REPLICAS });
  } catch (err: any) {
    if (err.code === '404' || err.message?.includes('not found')) {
      kvStore = await new Kvm(client).create(LANE_BUCKET, {
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

export async function ensureLaneStoreHA(): Promise<void> {
  await getKV();
}

function laneKey(stream: string, consumer: string) {
  return `${stream}\x00${consumer}`;
}

function laneAliasKey(stream: string, consumer: string) {
  return `${stream}.${consumer}`;
}

const prevSamples = new Map<string, DeliverySample>();
let currentLanes: LaneView[] = [];
let lastError = '';
let samplerTimer: ReturnType<typeof setInterval> | null = null;
let sampling: Promise<void> | null = null;
let lastLoadAt = 0;
let lastCollectionAt = 0;

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
  return formatDeliveryRate(hasRate ? rate : null);
}

function markAliasesDirty() {
  aliasCacheExpires = 0;
}

async function loadAliases(): Promise<Map<string, string>> {
  if (aliasCacheExpires > Date.now()) return aliasCache;

  const kv = await getKV();
  const aliases = new Map<string, string>();
  try {
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
    if (err.code !== '404') {
      logger.warn({ err }, 'lane alias fetch error');
      throw err;
    }
  }

  aliasCache = aliases;
  aliasCacheExpires = Date.now() + ALIAS_CACHE_TTL_MS;
  return aliasCache;
}

function hasFreshLaneSample() {
  if (currentLanes.length === 0) return false;
  return Date.now() - lastCollectionAt < SAMPLE_INTERVAL_MS;
}

function reuseLaneSample(force: boolean) {
  if (force) return false;
  return hasFreshLaneSample();
}

async function collectLanes(force = false) {
  if (sampling) return sampling;
  // Polling clients and the timer share one cadence. Sampling every warm read
  // creates tiny rate windows and makes a steady consumer look bursty.
  if (reuseLaneSample(force)) return;
  lastCollectionAt = Date.now();
  sampling = collectLanesOnce().finally(() => {
    sampling = null;
  });
  return sampling;
}

interface LaneRow {
  stream: string;
  consumer: string;
  filter: string;
  ephemeral: boolean;
  category: string;
  group: string;
  pending: number;
  ackPending: number;
  maxAckPend: number;
  redelivered: number;
  rate: number;
  hasRate: boolean;
  activity: ReturnType<typeof consumerActivity>;
}

// Fetch every page concurrently across streams and report partial failures.
async function listStreamConsumers(manager: Awaited<ReturnType<typeof jsm>>) {
  const streams = await allPages(manager.streams.list());
  const listed = await Promise.allSettled(
    streams.map(async (stream) => ({
      streamName: stream.config.name,
      consumers: await allPages(manager.consumers.list(stream.config.name))
    }))
  );
  return {
    streamsSeen: streams.length,
    failures: listed.filter((r) => r.status === 'rejected').length,
    fulfilled: listed
      .filter((r) => r.status === 'fulfilled')
      .map((r) => (r as PromiseFulfilledResult<{ streamName: string; consumers: any[] }>).value)
  };
}

function sampleRate(key: string, deliveredSeq: number, created: string, now: number): { rate: number; hasRate: boolean } {
  const prev = prevSamples.get(key);
  const current = { delivered: deliveredSeq, created, at: now };
  prevSamples.set(key, current);
  const rate = deliveryRate(prev, current);
  return { rate: rate ?? 0, hasRate: rate !== null };
}

function laneRowOf(streamName: string, ci: any, now: number): LaneRow {
  const filter = ci.config.filter_subject || ci.config.filter_subjects?.join(', ') || '';
  const ephemeral = !ci.config.durable_name;
  const activity = consumerActivity(ci);
  const { rate, hasRate } = sampleRate(laneKey(streamName, ci.name), ci.delivered.consumer_seq, ci.created, now);
  return {
    stream: streamName,
    consumer: ci.name,
    filter,
    ephemeral,
    category: laneCategory(streamName, ephemeral),
    group: laneGroup(ci.name, filter, ephemeral),
    pending: ci.num_pending,
    ackPending: ci.num_ack_pending,
    maxAckPend: ci.config.max_ack_pending || 0,
    redelivered: ci.num_redelivered,
    rate,
    hasRate,
    activity
  };
}

function compareLanes(a: LaneRow, b: LaneRow): number {
  return (
    categoryRank(a.category) - categoryRank(b.category) ||
    a.stream.localeCompare(b.stream) ||
    a.filter.localeCompare(b.filter) ||
    a.consumer.localeCompare(b.consumer)
  );
}

function laneViewOf(r: LaneRow, aliases: Map<string, string>): LaneView {
  return {
    stream: r.stream,
    consumer: r.consumer,
    display: displayName(aliases.get(laneAliasKey(r.stream, r.consumer)), r.group, r.consumer, r.ephemeral),
    subject: r.filter,
    category: r.category,
    ephemeral: r.ephemeral,
    pending: r.pending,
    inFlight: inFlightText(r.ackPending, r.maxAckPend),
    rate: rateText(r.rate, r.hasRate),
    redelivered: r.redelivered,
    ...r.activity,
    ratePerSecond: r.hasRate ? r.rate : null
  };
}

function pruneStaleBaselines(seen: Set<string>) {
  for (const key of prevSamples.keys()) {
    if (!seen.has(key)) prevSamples.delete(key);
  }
}

async function collectLanesOnce() {
  try {
    const manager = await jsm();
    let aliasError = '';
    const [aliases, listing] = await Promise.all([
      loadAliases().catch((err: any) => {
        aliasError = 'display aliases unavailable: ' + (err.message || String(err));
        return aliasCache;
      }),
      listStreamConsumers(manager)
    ]);
    const { streamsSeen, failures, fulfilled } = listing;

    if (streamsSeen === 0) {
      lastError = "JetStream API unreachable: no streams returned (broker unreachable or account lacks $JS.API access)";
      return;
    }

    const now = Date.now();
    const rows = fulfilled.flatMap(({ streamName, consumers }) =>
      consumers.map((ci) => laneRowOf(streamName, ci, now))
    );
    if (failures === 0) {
      pruneStaleBaselines(new Set(rows.map((r) => laneKey(r.stream, r.consumer))));
    }

    rows.sort(compareLanes);
    currentLanes = rows.map((r) => laneViewOf(r, aliases));
    lastError = [failures > 0 ? `partial listing: ${failures} of ${streamsSeen} streams unreadable` : '', aliasError].filter(Boolean).join('; ');
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
}

export async function loadLanes(): Promise<LanesResult> {
  if (DEMO) {
    const { sampleLanes } = await import('./demo-data');
    return { lanes: sampleLanes, degraded: false, notice: '' };
  }
  ensureSampler();
  const pending = collectLanes();
  if (currentLanes.length === 0) await pending;
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
      collectLanes(true);
      return { ok: true, notice: 'alias cleared' };
    }
    await kv.put(key, new TextEncoder().encode(alias.slice(0, 48)));
    markAliasesDirty();
    collectLanes(true);
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
    collectLanes(true);
    return { ok: true, notice: `created permanent lane ${name}` };
  } catch (err: any) {
    return { ok: false, error: 'make-permanent failed: ' + err.message };
  }
}

export async function laneDelete(stream: string, consumer: string): Promise<LaneMutationResult> {
  try {
    const manager = await jsm();
    const info = await manager.consumers.info(stream, consumer);
    if (!info.config.deliver_subject) {
      return { ok: false, error: 'refused: pull consumer activity cannot be proven absent from JetStream telemetry' };
    }
    if (info.push_bound) {
      return { ok: false, error: 'refused: lane is bound to a running consumer, not an orphan' };
    }
    await manager.consumers.delete(stream, consumer);
    const kv = await getKV();
    await kv.delete(laneAliasKey(stream, consumer)).catch(() => {});
    markAliasesDirty();
    collectLanes(true);
    return { ok: true, notice: `deleted orphan lane ${consumer}` };
  } catch (err: any) {
    return { ok: false, error: 'delete failed: ' + err.message };
  }
}
