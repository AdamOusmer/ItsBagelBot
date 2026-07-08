// Timers store: a broadcaster's repeating chat messages, stream-only.
//
// Unlike channel points there is no external Twitch entity to CRUD — the
// "timers" module blob (the same modules service every other feature uses) is
// the sole source of truth. sesame arms every enabled timer on stream.online,
// disarms them on stream.offline, and fires each off its own Valkey key expiry
// (app/sesame/engine/timers_valkey.go), re-reading this same blob every cycle.
import { randomUUID } from 'node:crypto';
import type { TimerDef } from '@bagel/shared';
import { listModules, upsertModule } from './commands-store';

const TIMERS_MODULE = 'timers';

export interface TimersView {
  enabled: boolean;
  timers: TimerDef[];
}

export type TimerResult = { ok: true; timer?: TimerDef } | { ok: false; error?: string };

// readTimers loads the current blob (enable flag + timer records).
export async function readTimers(userId: string): Promise<TimersView> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === TIMERS_MODULE);
  const enabled = row ? row.is_enabled : false;
  const configs = (row?.configs ?? {}) as { timers?: TimerDef[] };
  return { enabled, timers: Array.isArray(configs.timers) ? configs.timers : [] };
}

async function writeTimers(userId: string, enabled: boolean, timers: TimerDef[]): Promise<void> {
  await upsertModule(userId, TIMERS_MODULE, enabled, timers.length ? { timers } : {});
}

export async function createTimer(userId: string, draft: TimerDef): Promise<TimerResult> {
  const created: TimerDef = { ...draft, id: draft.id || randomUUID() };
  const cur = await readTimers(userId);
  // Adding the first timer turns the module on; later adds preserve whatever
  // enable state the broadcaster set.
  const enabled = cur.timers.length === 0 ? true : cur.enabled;
  await writeTimers(userId, enabled, [...cur.timers, created]);
  return { ok: true, timer: created };
}

export async function updateTimer(userId: string, draft: TimerDef): Promise<TimerResult> {
  if (!draft.id) return { ok: false, error: 'missing timer id' };
  const cur = await readTimers(userId);
  const timers = cur.timers.map((t) => (t.id === draft.id ? draft : t));
  await writeTimers(userId, cur.enabled, timers);
  return { ok: true, timer: draft };
}

export async function deleteTimer(userId: string, timerId: string): Promise<TimerResult> {
  const cur = await readTimers(userId);
  await writeTimers(
    userId,
    cur.enabled,
    cur.timers.filter((t) => t.id !== timerId)
  );
  return { ok: true };
}

// setTimersEnabled flips the whole module on/off (whether sesame arms any
// timer at all) without touching the timers themselves.
export async function setTimersEnabled(userId: string, enabled: boolean): Promise<void> {
  const cur = await readTimers(userId);
  await writeTimers(userId, enabled, cur.timers);
}
