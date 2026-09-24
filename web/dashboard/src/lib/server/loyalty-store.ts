// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import type { CounterDef, CounterEntryView, CounterScope, LoyaltyConfig, LoyaltyStanding } from '@bagel/kit';
import { blankLoyaltyConfig, COUNTER_SCOPES, MOD } from '@bagel/kit';
import { SUB } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob } from './module-blob';

const LOYALTY_MODULE = MOD.loyalty;

export interface LoyaltyView {
  enabled: boolean;
  config: LoyaltyConfig;
}

interface BalanceWire {
  viewer_id: string;
  viewer_login?: string;
  viewer_name?: string;
  points: number;
  watch_seconds: number;
}

interface CounterWire {
  name: string;
  scope: string;
  value: number;
}

interface EntryWire {
  viewer_id: string;
  viewer_login?: string;
  viewer_name?: string;
  command?: string;
  value: number;
}

interface LoyaltyReplyWire {
  balance?: BalanceWire;
  top?: BalanceWire[];
  counter?: CounterWire;
  counters?: CounterWire[];
  entries?: EntryWire[];
  found?: boolean;
  error?: string;
}

function callLoyalty(verb: string, req: Record<string, unknown>): Promise<LoyaltyReplyWire> {
  return rpc<LoyaltyReplyWire>(`${SUB.loyalty}.${verb}`, req, 4000);
}

const RATE_KEYS = [
  'subPoints',
  'resubPoints',
  'giftSubPoints',
  'cheerPointsPer100',
  'watchPointsPerTick',
  'modSetPoints',
  'modAdjustPoints',
  'viewerTransfers'
] as const satisfies readonly (keyof LoyaltyConfig)[];

function rate(v: unknown): number {
  return Number(v ?? 0) || 0;
}

export async function readLoyalty(userId: string): Promise<LoyaltyView> {
  const { enabled, configs: raw } = await readModuleBlob<Partial<LoyaltyConfig>>(userId, LOYALTY_MODULE);
  const config: LoyaltyConfig = { ...blankLoyaltyConfig(), pointsName: String(raw.pointsName ?? '') };
  for (const key of RATE_KEYS) config[key] = rate(raw[key]);
  return { enabled, config };
}

export async function writeLoyalty(userId: string, enabled: boolean, config: LoyaltyConfig): Promise<void> {
  await upsertModule(userId, LOYALTY_MODULE, enabled, config);
}

function toScope(raw: string | undefined): CounterScope {
  return COUNTER_SCOPES.includes(raw as CounterScope) ? (raw as CounterScope) : 'channel';
}

export async function listCounters(userId: string): Promise<CounterDef[]> {
  const reply = await callLoyalty('counter.list', { user_id: userId });
  return (reply.counters ?? []).map((c) => ({
    name: c.name,
    scope: toScope(c.scope),
    value: c.value
  }));
}

export async function createCounter(userId: string, name: string, scope: CounterScope): Promise<CounterDef> {
  const reply = await callLoyalty('counter.create', { user_id: userId, name, scope });
  const c = reply.counter;
  if (!c) throw new Error('empty counter reply');
  return { name: c.name, scope: toScope(c.scope), value: c.value };
}

export interface CounterTarget {
  viewerId?: string;
  command?: string;
  viewerLogin?: string;
}

// Untargeted on an entry-scoped counter, 0 resets every stored bucket.
export async function setCounter(userId: string, name: string, value: number, target: CounterTarget = {}): Promise<boolean> {
  const reply = await callLoyalty('counter.set', {
    user_id: userId,
    name,
    value,
    viewer_id: target.viewerId || undefined,
    command: target.command || undefined,
    viewer_login: target.viewerLogin || undefined
  });
  return reply.found === true;
}

export async function getCounter(userId: string, name: string): Promise<CounterDef | null> {
  const reply = await callLoyalty('counter.get', { user_id: userId, name });
  if (reply.found !== true || !reply.counter) return null;
  return { name: reply.counter.name, scope: toScope(reply.counter.scope), value: reply.counter.value };
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

export async function renameCounter(userId: string, name: string, newName: string): Promise<boolean> {
  const reply = await callLoyalty('counter.rename', { user_id: userId, name, new_name: newName });
  return reply.found === true;
}

export async function deleteCounter(userId: string, name: string): Promise<void> {
  await callLoyalty('counter.delete', { user_id: userId, name });
}

export async function deleteCounterEntry(userId: string, name: string, target: CounterTarget): Promise<boolean> {
  const reply = await callLoyalty('counter.entry.delete', {
    user_id: userId,
    name,
    viewer_id: target.viewerId || undefined,
    command: target.command || undefined
  });
  return reply.found === true;
}

export async function counterEntries(userId: string, name: string, limit = 25): Promise<CounterEntryView[]> {
  const reply = await callLoyalty('counter.entries', { user_id: userId, name, limit });
  return (reply.entries ?? []).map((e) => ({
    viewerId: e.viewer_id,
    viewerLogin: e.viewer_login ?? '',
    viewerName: e.viewer_name ?? '',
    command: e.command ?? '',
    value: e.value
  }));
}

export async function topStandings(userId: string, limit = 10): Promise<LoyaltyStanding[]> {
  const reply = await callLoyalty('top.get', { user_id: userId, limit });
  return (reply.top ?? []).map((b) => ({
    viewerId: b.viewer_id,
    viewerLogin: b.viewer_login ?? '',
    viewerName: b.viewer_name ?? '',
    points: b.points,
    watchSeconds: b.watch_seconds
  }));
}
