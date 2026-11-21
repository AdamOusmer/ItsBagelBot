// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { jsm, js } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { Kvm, type KV } from '@nats-io/kv';
import { dev } from '$app/env';
import { allPages, consumerActivity, deliveryRate, formatDeliveryRate, type DeliverySample } from './lane-telemetry';

// Do not import access.ts: it pulls in services.ts, an import cycle through hooks.server.ts at boot.
const DEMO = dev && process.env.DEMO === '1';

type LaneManager = Awaited<ReturnType<typeof jsm>>;
type ConsumerInfo = Awaited<ReturnType<LaneManager['consumers']['info']>>;
type LaneStream = ConsumerInfo['stream_name'];
type ConsumerName = ConsumerInfo['name'];
type LaneCategory = 'system' | 'projection' | 'ephemeral';

export interface LaneView {
  stream: LaneStream;
  consumer: ConsumerName;
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

async function ensureLaneReplicas(manager: LaneManager) {
  const { config } = await manager.streams.info(LANE_STREAM);
  if (config.num_replicas === NATS_REPLICAS) return;
  await manager.streams.update(LANE_STREAM, { ...config, num_replicas: NATS_REPLICAS });
}

async function getKV(): Promise<KV> {
  if (kvStore) return kvStore;
  const client = await js();
  const manager = await jsm();
  try {
    await ensureLaneReplicas(manager);
    kvStore = await new Kvm(client).create(LANE_BUCKET, { history: 1, replicas: NATS_REPLICAS });
  } catch (err: any) {
    if (err.code !== '404' && !err.message?.includes('not found')) throw err;
    kvStore = await new Kvm(client).create(LANE_BUCKET, {
      history: 1,
      replicas: NATS_REPLICAS,
      description: 'admin lane display aliases'
    });
  }
  return kvStore;
}

export async function ensureLaneStoreHA(): Promise<void> {
  await getKV();
}

type LaneIdentity = Pick<LaneView, 'stream' | 'consumer'>;

function laneKey(lane: LaneIdentity) {
  return `${lane.stream}\x00${lane.consumer}`;
}

function laneAliasKey(lane: LaneIdentity) {
  return `${lane.stream}.${lane.consumer}`;
}

const deliverySamples = new Map<string, DeliverySample>();
let currentLanes: LaneView[] = [];
let lastError = '';
let samplerTimer: ReturnType<typeof setInterval> | null = null;
let sampling: Promise<void> | null = null;
let lastLoadAt = 0;
let lastCollectionAt = 0;

const SAMPLE_INTERVAL_MS = 5_000;
const SAMPLE_IDLE_MS = 60_000;
const ALIAS_CACHE_TTL_MS = 30_000;
const ALIAS_READ_BATCH_SIZE = 50;

let aliasCache = new Map<string, string>();
let aliasCacheExpires = 0;

function subjectToken(subject: string) {
  return subject.replace(/[.*>]/g, '_');
}

function laneGroup(consumer: ConsumerName, subject: string, ephemeral: boolean) {
  if (ephemeral) return '';
  if (!subject) return consumer;
  const suffix = `_${subjectToken(subject)}`;
  return consumer.endsWith(suffix) ? consumer.slice(0, -suffix.length) : consumer;
}

function laneCategory(stream: LaneStream, ephemeral: boolean): LaneCategory {
  if (ephemeral) return 'ephemeral';
  if (stream.startsWith('TWITCH_OUTGRESS')) return 'system';
  return 'projection';
}

function categoryRank(category: LaneCategory) {
  switch (category) {
    case 'system': return 0;
    case 'projection': return 1;
    default: return 2;
  }
}

function displayName(row: LaneRow, alias: string | undefined) {
  if (alias) return alias;
  if (row.ephemeral) return 'ephemeral';
  return row.group || row.consumer;
}

function inFlightText({ ackPending, maxAckPending }: LaneActivity) {
  if (maxAckPending > 0) return `${ackPending} / ${maxAckPending}`;
  return `${ackPending}`;
}

function refreshAliases() {
  aliasCacheExpires = 0;
  collectLanes(true);
}

async function addAliasEntries(aliases: Map<string, string>, kv: KV, keys: string[]) {
  const entries = await Promise.all(keys.map((key) => kv.get(key)));
  entries.forEach((entry, index) => {
    if (entry) aliases.set(keys[index], entry.string());
  });
}

async function loadAliases(): Promise<Map<string, string>> {
  if (aliasCacheExpires > Date.now()) return aliasCache;

  const kv = await getKV();
  const aliases = new Map<string, string>();
  try {
    const keys: string[] = [];
    const keysIter = await kv.keys();
    for await (const key of keysIter) keys.push(key);
    for (let start = 0; start < keys.length; start += ALIAS_READ_BATCH_SIZE) {
      await addAliasEntries(aliases, kv, keys.slice(start, start + ALIAS_READ_BATCH_SIZE));
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

async function collectLanes(force = false) {
  if (sampling) return sampling;
  if (!force && hasFreshLaneSample()) return;
  lastCollectionAt = Date.now();
  sampling = collectLanesOnce().finally(() => {
    sampling = null;
  });
  return sampling;
}

type LaneActivity = ReturnType<typeof consumerActivity>;

interface LaneRow extends LaneIdentity {
  subject: string;
  ephemeral: boolean;
  category: LaneCategory;
  group: string;
  pending: number;
  redelivered: number;
  rate: number | null;
  activity: LaneActivity;
}

async function listStreamConsumers(manager: LaneManager) {
  const streams = await allPages(manager.streams.list());
  const listed = await Promise.allSettled(
    streams.map(async (stream) => ({
      streamName: stream.config.name,
      consumers: await allPages(manager.consumers.list(stream.config.name))
    }))
  );
  const fulfilled = listed.flatMap((result) => result.status === 'fulfilled' ? [result.value] : []);
  return {
    streamsSeen: streams.length,
    failures: listed.length - fulfilled.length,
    fulfilled
  };
}

function sampleRate(lane: LaneIdentity, current: DeliverySample): number | null {
  const key = laneKey(lane);
  const previous = deliverySamples.get(key);
  deliverySamples.set(key, current);
  return deliveryRate(previous, current);
}

function laneRowOf(stream: LaneStream, info: ConsumerInfo, at: number): LaneRow {
  const subject = info.config.filter_subject || info.config.filter_subjects?.join(', ') || '';
  const ephemeral = !info.config.durable_name;
  const activity = consumerActivity(info);
  const identity = { stream, consumer: info.name };
  return {
    ...identity,
    subject,
    ephemeral,
    category: laneCategory(stream, ephemeral),
    group: laneGroup(info.name, subject, ephemeral),
    pending: info.num_pending,
    redelivered: info.num_redelivered,
    rate: sampleRate(identity, { delivered: activity.delivered, created: info.created, at }),
    activity
  };
}

function compareLanes(a: LaneRow, b: LaneRow): number {
  return (
    categoryRank(a.category) - categoryRank(b.category) ||
    a.stream.localeCompare(b.stream) ||
    a.subject.localeCompare(b.subject) ||
    a.consumer.localeCompare(b.consumer)
  );
}

function laneViewOf(row: LaneRow, aliases: Map<string, string>): LaneView {
  return {
    stream: row.stream,
    consumer: row.consumer,
    display: displayName(row, aliases.get(laneAliasKey(row))),
    subject: row.subject,
    category: row.category,
    ephemeral: row.ephemeral,
    pending: row.pending,
    inFlight: inFlightText(row.activity),
    rate: formatDeliveryRate(row.rate),
    redelivered: row.redelivered,
    ...row.activity,
    ratePerSecond: row.rate
  };
}

function pruneStaleBaselines(seen: Set<string>) {
  deliverySamples.forEach((_, key) => {
    if (!seen.has(key)) deliverySamples.delete(key);
  });
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
      consumers.map((info) => laneRowOf(streamName, info, now))
    );
    if (failures === 0) {
      pruneStaleBaselines(new Set(rows.map(laneKey)));
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
      clearInterval(samplerTimer ?? undefined);
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

export async function laneAlias(stream: LaneStream, consumer: ConsumerName, alias: string): Promise<LaneMutationResult> {
  try {
    const kv = await getKV();
    const key = laneAliasKey({ stream, consumer });
    if (alias) await kv.put(key, new TextEncoder().encode(alias.slice(0, 48)));
    else await kv.delete(key);
    refreshAliases();
    return { ok: true, notice: alias ? 'renamed to ' + alias : 'alias cleared' };
  } catch (err: any) {
    return { ok: false, error: 'rename failed: ' + err.message };
  }
}

export async function laneDurable(stream: LaneStream, consumer: ConsumerName): Promise<LaneMutationResult> {
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

export async function laneDelete(stream: LaneStream, consumer: ConsumerName): Promise<LaneMutationResult> {
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
    await kv.delete(laneAliasKey({ stream, consumer })).catch(() => {});
    refreshAliases();
    return { ok: true, notice: `deleted orphan lane ${consumer}` };
  } catch (err: any) {
    return { ok: false, error: 'delete failed: ' + err.message };
  }
}
