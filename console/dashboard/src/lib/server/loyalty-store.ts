// Loyalty store: the points economy's two homes.
//
//   - The rates (points per sub/cheer/watch tick, the currency's name) live in
//     the "loyalty" module blob — the same modules service every other feature
//     uses; sesame re-reads it on every accrual.
//   - Standings and counters live in the loyalty service, reached over NATS
//     RPC (bagel.rpc.loyalty.*). Sesame is the writer (batched deltas); the
//     dashboard reads and runs the management verbs.
import { rpc } from '@bagel/shared/server/nats';
import type { CounterDef, CounterEntryView, CounterScope, LoyaltyConfig, LoyaltyStanding } from '@bagel/shared';
import { blankLoyaltyConfig, COUNTER_SCOPES } from '@bagel/shared';
import { SUB } from './services';
import { listModules, upsertModule } from './commands-store';

const LOYALTY_MODULE = 'loyalty';

export interface LoyaltyView {
  enabled: boolean;
  config: LoyaltyConfig;
}

// Wire mirrors of the Go loyaltyrpc shapes (snake_case).
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

// readLoyalty loads the module blob (enable flag + rates).
export async function readLoyalty(userId: string): Promise<LoyaltyView> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === LOYALTY_MODULE);
  const enabled = row ? row.is_enabled : false;
  const raw = (row?.configs ?? {}) as Partial<LoyaltyConfig>;
  const config: LoyaltyConfig = {
    ...blankLoyaltyConfig(),
    pointsName: String(raw.pointsName ?? ''),
    subPoints: Number(raw.subPoints ?? 0) || 0,
    resubPoints: Number(raw.resubPoints ?? 0) || 0,
    giftSubPoints: Number(raw.giftSubPoints ?? 0) || 0,
    cheerPointsPer100: Number(raw.cheerPointsPer100 ?? 0) || 0,
    watchPointsPerTick: Number(raw.watchPointsPerTick ?? 0) || 0
  };
  return { enabled, config };
}

export async function writeLoyalty(userId: string, enabled: boolean, config: LoyaltyConfig): Promise<void> {
  await upsertModule(userId, LOYALTY_MODULE, enabled, config);
}

function toScope(raw: string | undefined): CounterScope {
  return COUNTER_SCOPES.includes(raw as CounterScope) ? (raw as CounterScope) : 'channel';
}

// listCounters returns the channel's counter definitions.
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

// setCounter writes an absolute channel value; on entry-scoped counters a zero
// resets every stored bucket (the service's reset semantics).
export async function setCounter(userId: string, name: string, value: number): Promise<boolean> {
  const reply = await callLoyalty('counter.set', { user_id: userId, name, value });
  return reply.found === true;
}

// renameCounter moves a counter (and its stored buckets) to a new name;
// false means no counter carries the old name.
export async function renameCounter(userId: string, name: string, newName: string): Promise<boolean> {
  const reply = await callLoyalty('counter.rename', { user_id: userId, name, new_name: newName });
  return reply.found === true;
}

export async function deleteCounter(userId: string, name: string): Promise<void> {
  await callLoyalty('counter.delete', { user_id: userId, name });
}

// counterEntries lists an entry-scoped counter's buckets, highest first.
export async function counterEntries(userId: string, name: string, limit = 25): Promise<CounterEntryView[]> {
  const reply = await callLoyalty('counter.entries', { user_id: userId, name, limit });
  return (reply.entries ?? []).map((e) => ({
    viewerId: e.viewer_id,
    viewerLogin: e.viewer_login ?? '',
    command: e.command ?? '',
    value: e.value
  }));
}

// topStandings returns the channel's points leaderboard.
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
