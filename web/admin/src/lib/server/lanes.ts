// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { jsm, js } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { Kvm, type KV } from '@nats-io/kv';
import { allPages, consumerActivity, deliveryRate, formatDeliveryRate, type DeliverySample } from './lane-telemetry';

// Do not import access.ts: it pulls in services.ts, an import cycle through hooks.server.ts at boot.
type LaneManager = Awaited<ReturnType<typeof jsm>>;
type LaneClient = Awaited<ReturnType<typeof js>>;
type ConsumerInfo = Awaited<ReturnType<LaneManager['consumers']['info']>>;
type LaneStream = ConsumerInfo['stream_name'];
type ConsumerName = ConsumerInfo['name'];
export type LaneCategory = 'system' | 'projection' | 'ephemeral';

export interface LaneView {
  stream: LaneStream;
  consumer: ConsumerName;
  display: string;
  alias?: string;
  defaultDisplay?: string;
  subject: string;
  category: LaneCategory;
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
  error?: string;
}

const LANE_BUCKET = 'admin_lanes';
const LANE_STREAM = `KV_${LANE_BUCKET}`;
const NATS_REPLICAS = 3;

const stores = new WeakMap<LaneClient, Promise<KV>>();

async function laneStoreInfo(manager: LaneManager) {
  try {
    return await manager.streams.info(LANE_STREAM);
  } catch (error) {
    if (error instanceof Error && error.name === 'StreamNotFoundError') return undefined;
    throw error;
  }
}

async function openLaneStore(client: LaneClient): Promise<KV> {
  const manager = await jsm();
  const kvm = new Kvm(client);
  const info = await laneStoreInfo(manager);
  if (!info) {
    return kvm.create(LANE_BUCKET, {
      history: 1,
      replicas: NATS_REPLICAS,
      description: 'admin lane display aliases'
    });
  }
  if (info.config.num_replicas !== NATS_REPLICAS) {
    await manager.streams.update(LANE_STREAM, { num_replicas: NATS_REPLICAS });
  }
  return kvm.open(LANE_BUCKET, { allow_direct: info.config.allow_direct });
}

async function getKV(): Promise<KV> {
  const client = await js();
  const existing = stores.get(client);
  if (existing) return existing;
  const store = openLaneStore(client).catch((error) => {
    stores.delete(client);
    throw error;
  });
  stores.set(client, store);
  return store;
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
let current: LanesResult | undefined;
let samplerTimer: ReturnType<typeof setInterval> | null = null;
let sampling: Promise<void> | null = null;
let lastLoadAt = 0;
let lastCollectionAt = Number.NEGATIVE_INFINITY;

const SAMPLE_INTERVAL_MS = 5_000;
const SAMPLE_IDLE_MS = 60_000;
const ALIAS_CACHE_TTL_MS = 30_000;

let aliasCache = new Map<string, string>();
let aliasCacheExpires = 0;

function subjectToken(subject: string) {
  return subject.replace(/[.*>]/g, '_');
}

function laneGroup(consumer: ConsumerName, subject: string) {
  if (!subject) return consumer;
  const suffix = `_${subjectToken(subject)}`;
  if (!consumer.endsWith(suffix)) return consumer;
  return consumer.slice(0, -suffix.length) || consumer;
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

function inFlightText({ ackPending, maxAckPending }: LaneActivity) {
  if (maxAckPending > 0) return `${ackPending} / ${maxAckPending}`;
  return `${ackPending}`;
}

async function loadAliases(): Promise<Map<string, string>> {
  if (aliasCacheExpires > Date.now()) return aliasCache;

  const kv = await getKV();
  const aliases = new Map<string, string>();
  const entries = await kv.history();
  for await (const entry of entries) {
    if (entry.operation === 'PUT') aliases.set(entry.key, entry.string());
    else aliases.delete(entry.key);
  }

  aliasCache = aliases;
  aliasCacheExpires = Date.now() + ALIAS_CACHE_TTL_MS;
  return aliasCache;
}

function hasFreshLaneSample() {
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

async function listStreamConsumers(manager: LaneManager) {
  const streamNames = await allPages(manager.streams.names());
  const listed = await Promise.allSettled(
    streamNames.map(async (streamName) => ({
      streamName,
      consumers: await allPages(manager.consumers.list(streamName))
    }))
  );
  const fulfilled: { streamName: LaneStream; consumers: ConsumerInfo[] }[] = [];
  const failedStreams = new Set<LaneStream>();
  listed.forEach((result, index) => {
    if (result.status === 'fulfilled') fulfilled.push(result.value);
    else failedStreams.add(streamNames[index]);
  });
  return { streamsSeen: streamNames.length, failedStreams, fulfilled };
}

function sampleRate(lane: LaneIdentity, current: DeliverySample): number | null {
  const key = laneKey(lane);
  const previous = deliverySamples.get(key);
  deliverySamples.set(key, current);
  return deliveryRate(previous, current);
}

function laneViewOf(stream: LaneStream, info: ConsumerInfo, at: number): LaneView {
  const subject = info.config.filter_subject || info.config.filter_subjects?.join(', ') || '';
  const ephemeral = !info.config.durable_name;
  const activity = consumerActivity(info);
  const identity = { stream, consumer: info.name };
  const display = ephemeral ? 'ephemeral' : laneGroup(info.name, subject);
  const rate = sampleRate(identity, { delivered: activity.delivered, created: info.created, at });
  return {
    ...identity,
    display,
    defaultDisplay: display,
    subject,
    ephemeral,
    category: laneCategory(stream, ephemeral),
    pending: info.num_pending,
    redelivered: info.num_redelivered,
    inFlight: inFlightText(activity),
    rate: formatDeliveryRate(rate),
    ratePerSecond: rate,
    ...activity
  };
}

function compareLanes(a: LaneView, b: LaneView): number {
  return (
    categoryRank(a.category) - categoryRank(b.category) ||
    a.stream.localeCompare(b.stream) ||
    a.subject.localeCompare(b.subject) ||
    a.consumer.localeCompare(b.consumer)
  );
}

function withAlias(lane: LaneView, aliases: Map<string, string>): LaneView {
  const alias = aliases.get(laneAliasKey(lane)) ?? '';
  return {
    ...lane,
    alias,
    display: alias || lane.defaultDisplay || lane.display
  };
}

function pruneStaleBaselines(seen: Set<string>) {
  deliverySamples.forEach((_, key) => {
    if (!seen.has(key)) deliverySamples.delete(key);
  });
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

async function readAliases() {
  try {
    return { aliases: await loadAliases(), error: '' };
  } catch (error) {
    logger.warn({ err: error }, 'lane alias fetch error');
    return { aliases: aliasCache, error: `display aliases unavailable: ${errorMessage(error)}` };
  }
}

function laneSnapshot(lanes: LaneView[], ...errors: string[]): LanesResult {
  const notice = errors.filter(Boolean).join(' · ');
  return { lanes, degraded: Boolean(notice), notice };
}

type LaneListing = Awaited<ReturnType<typeof listStreamConsumers>>;

function listingSnapshot(listing: LaneListing, aliases: Map<string, string>, aliasError: string) {
  const { streamsSeen, failedStreams, fulfilled } = listing;
  const failures = failedStreams.size;
  const listingError = failures > 0 ? `partial listing: ${failures} of ${streamsSeen} streams unreadable` : '';
  const now = Date.now();
  const freshLanes = fulfilled.flatMap(({ streamName, consumers }) =>
    consumers.map((info) => laneViewOf(streamName, info, now))
  );
  const cachedLanes = current?.lanes.filter((lane) => failedStreams.has(lane.stream)) ?? [];
  const lanes = [...freshLanes, ...cachedLanes].map((lane) => withAlias(lane, aliases));
  pruneStaleBaselines(new Set(lanes.map(laneKey)));
  return laneSnapshot(lanes.sort(compareLanes), listingError, aliasError);
}

async function collectLanesOnce() {
  try {
    const [aliasResult, listing] = await Promise.allSettled([
      readAliases(),
      jsm().then(listStreamConsumers)
    ]);
    if (aliasResult.status === 'rejected') throw aliasResult.reason;
    const { aliases, error } = aliasResult.value;
    if (listing.status === 'rejected') {
      const lanes = current?.lanes.map((lane) => withAlias(lane, aliases)) ?? [];
      current = laneSnapshot(lanes, errorMessage(listing.reason), error);
      return;
    }
    current = listingSnapshot(listing.value, aliases, error);
  } catch (error) {
    current = laneSnapshot(current?.lanes ?? [], errorMessage(error));
  }
}

async function refreshLanes(aliasesChanged: boolean) {
  await sampling;
  if (aliasesChanged) aliasCacheExpires = 0;
  await collectLanes(true);
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
    void collectLanes();
  }, SAMPLE_INTERVAL_MS);
}

export async function loadLanes(): Promise<LanesResult> {
  ensureSampler();
  const pending = collectLanes();
  if (!current || current.degraded) await pending;
  return current ?? laneSnapshot([]);
}

export async function laneAlias(stream: LaneStream, consumer: ConsumerName, alias: string): Promise<LaneMutationResult> {
  const kv = await getKV();
  const key = laneAliasKey({ stream, consumer });
  const name = alias.trim().slice(0, 48);
  if (name) await kv.put(key, new TextEncoder().encode(name));
  else await kv.delete(key);
  await refreshLanes(true);
  return { ok: true };
}

export async function laneDurable(stream: LaneStream, consumer: ConsumerName): Promise<LaneMutationResult> {
  const manager = await jsm();
  const info = await manager.consumers.info(stream, consumer);
  if (info.config.durable_name) return { ok: false, error: 'lane is already durable' };
  const name = `adminperm_${subjectToken(info.config.filter_subject || '')}`;
  await manager.consumers.add(stream, {
    ...info.config,
    name,
    durable_name: name,
    inactive_threshold: 0,
    description: 'operator-pinned permanent lane (admin)'
  });
  await refreshLanes(false);
  return { ok: true };
}

export async function laneDelete(stream: LaneStream, consumer: ConsumerName): Promise<LaneMutationResult> {
  const manager = await jsm();
  const info = await manager.consumers.info(stream, consumer);
  if (!consumerActivity(info).orphan) {
    return { ok: false, error: 'JetStream has not confirmed an unbound push consumer' };
  }
  if (!(await manager.consumers.delete(stream, consumer))) {
    return { ok: false, error: 'JetStream did not delete the consumer' };
  }
  await getKV().then((kv) => kv.delete(laneAliasKey({ stream, consumer }))).catch((error) => {
    logger.warn({ err: error }, 'lane alias cleanup error');
  });
  await refreshLanes(true);
  return { ok: true };
}
