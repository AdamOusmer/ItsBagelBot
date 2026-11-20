// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import type { CounterDef, CounterEntryView, CounterScope, LoyaltyConfig, LoyaltyStanding } from '@bagel/kit';
import { blankLoyaltyConfig, COUNTER_SCOPES, MOD } from '@bagel/kit';
import { SUB } from './services';
import { upsertModule } from './commands-store';
import { scheduleLeaderboardPurge } from './edge-purge';
import { readModuleBlob } from './module-blob';
import { parseCounterValue, readPointBalance } from '@bagel/kit/validation';

const LOYALTY_MODULE = MOD.loyalty;
const FREEZE_SET_TIMEOUT_MS = 30_000;

type BroadcasterId = string;

export interface LoyaltyView {
  enabled: boolean;
  config: LoyaltyConfig;
}

interface BalanceWire {
  viewer_id: string;
  viewer_login?: string;
  viewer_name?: string;
  points: number;
  points_exact?: string;
  watch_seconds: number;
  rank?: number;
}

interface CounterWire {
  name: string;
  scope: string;
  value: string | number;
}

interface EntryWire {
  viewer_id: string;
  viewer_login?: string;
  viewer_name?: string;
  command?: string;
  value: string | number;
}

interface FreezeStampWire {
  freeze_id?: string;
  frozen_as_of?: number;
  expires_at?: number;
}

interface FreezeViewWire extends FreezeStampWire {
  holders?: string[];
  block_transfer?: boolean;
  close_games?: boolean;
}

interface LoyaltyReplyWire extends FreezeStampWire {
  balance?: BalanceWire;
  top?: BalanceWire[];
  total?: number;
  counter?: CounterWire;
  counters?: CounterWire[];
  entries?: EntryWire[];
  freeze?: FreezeViewWire;
  found?: boolean;
  error?: string;
}

function callLoyalty(verb: string, req: Record<string, unknown>, timeoutMs = 4000): Promise<LoyaltyReplyWire> {
  return rpc<LoyaltyReplyWire>(`${SUB.loyalty}.${verb}`, req, timeoutMs);
}

const RATE_KEYS = [
  'subPoints',
  'resubPoints',
  'giftSubPoints',
  'cheerPointsPer100',
  'watchPointsPerTick',
  'modSetPoints',
  'modAdjustPoints',
  'viewerTransfers',
  'streamerPoints',
  'watchTier1Percent',
  'watchTier2Percent',
  'watchTier3Percent'
] as const satisfies readonly (keyof LoyaltyConfig)[];

function rate(v: unknown): number {
  return Number(v ?? 0) || 0;
}

export async function readLoyalty(userId: BroadcasterId): Promise<LoyaltyView> {
  const { enabled, configs: raw } = await readModuleBlob<Partial<LoyaltyConfig>>(userId, LOYALTY_MODULE);
  const config: LoyaltyConfig = {
    ...blankLoyaltyConfig(),
    pointsName: String(raw.pointsName ?? ''),
    watchSubBonus: raw.watchSubBonus === true
  };
  for (const key of RATE_KEYS) config[key] = rate(raw[key]);
  return { enabled, config };
}

export async function writeLoyalty(userId: BroadcasterId, enabled: boolean, config: LoyaltyConfig): Promise<void> {
  await upsertModule(userId, LOYALTY_MODULE, enabled, config);
}

function toScope(raw: string | undefined): CounterScope {
  return COUNTER_SCOPES.includes(raw as CounterScope) ? (raw as CounterScope) : 'channel';
}

function counterValue(raw: unknown): string {
  const value = parseCounterValue(raw);
  if (value === null) throw new Error('invalid counter value from loyalty');
  return value;
}

function toCounterDef(c: CounterWire): CounterDef {
  return { name: c.name, scope: toScope(c.scope), value: counterValue(c.value) };
}

export async function listCounters(userId: BroadcasterId): Promise<CounterDef[]> {
  const reply = await callLoyalty('counter.list', { user_id: userId });
  return (reply.counters ?? []).map(toCounterDef);
}

export async function createCounter(userId: BroadcasterId, name: string, scope: CounterScope): Promise<CounterDef> {
  const reply = await callLoyalty('counter.create', { user_id: userId, name, scope });
  const c = reply.counter;
  if (!c) throw new Error('empty counter reply');
  return toCounterDef(c);
}

export interface CounterTarget {
  viewerId?: string;
  command?: string;
  viewerLogin?: string;
}

// Untargeted on an entry-scoped counter, 0 resets every stored bucket.
export async function setCounter(userId: BroadcasterId, name: string, value: string, target: CounterTarget = {}): Promise<boolean> {
  const reply = await callLoyalty('counter.set', {
    user_id: userId,
    name,
    counter_value: value,
    viewer_id: target.viewerId || undefined,
    command: target.command || undefined,
    viewer_login: target.viewerLogin || undefined
  });
  return reply.found === true;
}

export async function getCounter(userId: BroadcasterId, name: string): Promise<CounterDef | null> {
  const reply = await callLoyalty('counter.get', { user_id: userId, name });
  if (reply.found !== true || !reply.counter) return null;
  return toCounterDef(reply.counter);
}

export async function resolveViewerId(login: string): Promise<string> {
  const reply = await rpc<{ target_id?: string; user_found?: boolean; error?: string }>(
    `${SUB.outgressRpc}.accountage.get`,
    { target_login: login },
    4000
  );
  if (reply.error || reply.user_found !== true) return '';
  return reply.target_id ?? '';
}

export async function renameCounter(userId: BroadcasterId, name: string, newName: string): Promise<boolean> {
  const reply = await callLoyalty('counter.rename', { user_id: userId, name, new_name: newName });
  return reply.found === true;
}

export async function deleteCounter(userId: BroadcasterId, name: string): Promise<void> {
  await callLoyalty('counter.delete', { user_id: userId, name });
}

export async function deleteCounterEntry(userId: BroadcasterId, name: string, target: CounterTarget): Promise<boolean> {
  const reply = await callLoyalty('counter.entry.delete', {
    user_id: userId,
    name,
    viewer_id: target.viewerId || undefined,
    command: target.command || undefined
  });
  return reply.found === true;
}

export async function counterEntries(userId: BroadcasterId, name: string, limit = 25): Promise<CounterEntryView[]> {
  const reply = await callLoyalty('counter.entries', { user_id: userId, name, limit });
  return (reply.entries ?? []).map((e) => ({
    viewerId: e.viewer_id,
    viewerLogin: e.viewer_login ?? '',
    viewerName: e.viewer_name ?? '',
    command: e.command ?? '',
    value: counterValue(e.value)
  }));
}

function toStanding(b: BalanceWire): LoyaltyStanding {
  return {
    viewerId: b.viewer_id,
    viewerLogin: b.viewer_login ?? '',
    viewerName: b.viewer_name ?? '',
    points: readPointBalance(b),
    watchSeconds: b.watch_seconds,
    rank: b.rank
  };
}

export async function resetPoints(userId: BroadcasterId): Promise<number> {
  const reply = await callLoyalty('balance.reset', { user_id: userId });
  scheduleLeaderboardPurge(userId);
  return reply.total ?? 0;
}

export interface FreezeStamp {
  id: string;
  asOf: number;
  expiresAt: number;
}

export interface FreezeOptions {
  minutes: number;
  blockTransfer: boolean;
  closeGames: boolean;
}

export interface FreezeView extends FreezeStamp, Omit<FreezeOptions, 'minutes'> {
  holders: string[];
}

function freezeStamp(wire: FreezeStampWire): FreezeStamp | null {
  const id = wire.freeze_id ?? '';
  const asOf = wire.frozen_as_of ?? 0;
  const expiresAt = wire.expires_at ?? 0;
  return id ? { id, asOf, expiresAt } : null;
}

function freezeView(wire: FreezeViewWire = {}): FreezeView | null {
  const stamp = freezeStamp(wire);
  if (!stamp) return null;
  return {
    ...stamp,
    holders: wire.holders ?? [],
    blockTransfer: wire.block_transfer ?? false,
    closeGames: wire.close_games ?? false
  };
}

export async function readFreeze(userId: BroadcasterId): Promise<FreezeView | null> {
  const reply = await callLoyalty('freeze.get', { user_id: userId });
  return freezeView(reply.freeze);
}

export async function freezeStandings(userId: BroadcasterId, options: FreezeOptions): Promise<FreezeView | null> {
  const reply = await callLoyalty('freeze.set', {
    user_id: userId,
    holder: 'mod',
    minutes: options.minutes,
    block_transfer: options.blockTransfer,
    close_games: options.closeGames
  }, FREEZE_SET_TIMEOUT_MS);
  return freezeView(reply.freeze);
}

export async function unfreezeStandings(userId: BroadcasterId, freezeId: FreezeStamp['id']): Promise<boolean> {
  const reply = await callLoyalty('freeze.release', { user_id: userId, holder: 'mod', freeze_id: freezeId });
  return reply.found === true;
}

export interface StandingsPage {
  rows: LoyaltyStanding[];
  total: number;
  freeze: FreezeStamp | null;
}

export interface StandingsPin {
  freezeId?: string;
  live?: boolean;
}

export interface StandingsPageRequest extends StandingsPin {
  limit: number;
  offset: number;
  viewerId?: string;
  viewerLogin?: string;
}

export function topStandings(userId: BroadcasterId, limit = 10, pin?: StandingsPin): Promise<StandingsPage> {
  return standingsPage(userId, { limit, offset: 0, ...pin });
}

export async function standingsPage(userId: BroadcasterId, req: StandingsPageRequest): Promise<StandingsPage> {
  const reply = await callLoyalty('top.get', {
    user_id: userId,
    limit: req.limit,
    offset: req.offset || undefined,
    viewer_id: req.viewerId || undefined,
    viewer_login: req.viewerLogin || undefined,
    freeze_id: req.freezeId,
    live: req.live
  });
  return { rows: (reply.top ?? []).map(toStanding), total: reply.total ?? 0, freeze: freezeStamp(reply) };
}

export interface RaffleStatus {
  accrual_health: 'healthy' | 'starting' | 'offline' | 'stalled';
  unpaid?: string[];
}

export function raffleStatus(userId: BroadcasterId): Promise<RaffleStatus> {
  return rpc<RaffleStatus>('rpc.draw.raffle.status', { user_id: userId, source: 'dashboard' }, 4000);
}
