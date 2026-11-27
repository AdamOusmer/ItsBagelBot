// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CounterDef, LoyaltyStanding } from '@bagel/kit';
import { SUB } from '#lib/server/services/subjects';
import { demoBoardViewers, demoCounters, demoCurrentLogins, demoEntries } from '../fixtures';
import type { Payload, Routes } from './types';

const counters = new Map<string, CounterDef>(demoCounters().map((c) => [c.name, c]));
const reconciled = new Map<string, string>();
const UNKNOWN_LOGINS = new Set(['nobody', 'nosuchuser']);

const nameOf = (payload: Payload) => String(payload.name ?? '');

function counterReply(c: CounterDef | undefined) {
  return c ? { found: true, counter: { name: c.name, scope: c.scope, value: c.value } } : { found: false };
}

function createCounter(payload: Payload) {
  const counter = { name: nameOf(payload), scope: payload.scope, value: '0' } as CounterDef;
  counters.set(counter.name, counter);
  return counterReply(counter);
}

const targetsEntry = (payload: Payload) => Boolean(payload.viewer_id || payload.command);

function setCounter(payload: Payload) {
  const c = counters.get(nameOf(payload));
  if (!c) return { found: false };
  if (!targetsEntry(payload)) c.value = String(payload.counter_value ?? '0');
  return { found: true };
}

function renameCounter(payload: Payload) {
  const c = counters.get(nameOf(payload));
  if (!c) return { found: false };
  counters.delete(c.name);
  counters.set(String(payload.new_name), { ...c, name: String(payload.new_name) });
  return { found: true };
}

function entries(payload: Payload) {
  const rows = demoEntries(nameOf(payload)).map((e) => ({
    viewer_id: e.viewerId,
    viewer_login: e.viewerLogin,
    viewer_name: e.viewerName,
    command: e.command,
    value: e.value
  }));
  return { entries: rows };
}

function ranked(): LoyaltyStanding[] {
  return demoBoardViewers()
    .sort((a, b) => Number(b.points) - Number(a.points) || Number(a.viewerId) - Number(b.viewerId))
    .map((viewer, i) => ({ ...viewer, viewerLogin: reconciled.get(viewer.viewerId) ?? viewer.viewerLogin, rank: i + 1 }));
}

function balanceWire(s: LoyaltyStanding) {
  return { viewer_id: s.viewerId, viewer_login: s.viewerLogin, viewer_name: s.viewerName, points_exact: String(s.points), points: Number(s.points), watch_seconds: s.watchSeconds, rank: s.rank };
}

function searched(payload: Payload): LoyaltyStanding[] {
  const id = String(payload.viewer_id ?? '');
  const login = String(payload.viewer_login ?? '');
  if (id && login) reconciled.set(id, login);
  const board = ranked();
  return id ? board.filter((s) => s.viewerId === id) : board.filter((s) => s.viewerLogin === login);
}

function topGet(payload: Payload) {
  const query = payload.viewer_id || payload.viewer_login;
  const offset = Number(payload.offset ?? 0);
  const rows = query ? searched(payload) : ranked().slice(offset, offset + Number(payload.limit ?? 10));
  return { top: rows.map(balanceWire), total: query ? rows.length : ranked().length };
}

function accountAge(payload: Payload) {
  const login = String(payload.target_login ?? '').toLowerCase();
  if (login.length < 4 || UNKNOWN_LOGINS.has(login)) return { user_found: false };
  const known = demoCurrentLogins.get(login) ?? ranked().find((s) => s.viewerLogin === login)?.viewerId;
  const id = known ?? String(100_000 + [...login].reduce((sum, ch) => sum + ch.charCodeAt(0), 0));
  return { user_found: true, target_id: id, login, display_name: login };
}

let freeze: { freeze_id: string; frozen_as_of: number; expires_at: number } | null = null;

function freezeSet(payload: Payload) {
  const now = Date.now();
  freeze = { freeze_id: `demo-freeze-${now}`, frozen_as_of: now, expires_at: now + Number(payload.minutes ?? 0) * 60_000 };
  return freeze;
}

function freezeGet() {
  if (freeze && freeze.expires_at <= Date.now()) freeze = null;
  return freeze ? { active: true, ...freeze } : { active: false };
}

const loyalty = (verb: string) => `${SUB.loyalty}.${verb}`;

export const loyaltyRoutes: Routes = {
  [loyalty('counter.list')]: () => ({ counters: [...counters.values()] }),
  [loyalty('counter.create')]: createCounter,
  [loyalty('counter.get')]: (payload) => counterReply(counters.get(nameOf(payload))),
  [loyalty('counter.set')]: setCounter,
  [loyalty('counter.rename')]: renameCounter,
  [loyalty('counter.delete')]: (payload) => ({ found: counters.delete(nameOf(payload)) }),
  [loyalty('counter.entry.delete')]: () => ({ found: true }),
  [loyalty('counter.entries')]: entries,
  [loyalty('top.get')]: topGet,
  [loyalty('freeze.get')]: freezeGet,
  [loyalty('freeze.set')]: freezeSet,
  [loyalty('freeze.release')]: () => {
    freeze = null;
    return { ok: true };
  },
  [loyalty('balance.reset')]: () => ({ total: ranked().length }),
  'rpc.draw.raffle.status': () => ({ error: 'unavailable', code: 'unavailable' }),
  [`${SUB.outgressRpc}.accountage.get`]: accountAge
};
