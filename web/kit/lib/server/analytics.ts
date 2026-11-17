// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Keep catalog validation aligned with Go or identifiers can leak into aggregate storage.
import { masterClient } from './valkey-master';
import { VIA } from '../via';

type Pipeline = {
  hincrby(key: string, field: string, n: number): Pipeline;
  expireat(key: string, at: number): Pipeline;
  exec(): Promise<unknown>;
};
type Client = { pipeline(): Pipeline };

const FLUSH_MS = 10_000;
const RETAIN_SECONDS = 7 * 24 * 60 * 60;

const NAME = /^[a-z0-9][a-z0-9_.-]{0,63}$/;

const one = (values: readonly string[]) => (v: string) => values.includes(v);
const shape = (re: RegExp) => (v: string) => re.test(v);

const CATALOG: Record<string, ((v: string) => boolean)[]> = {
  'auth.login': [one(['started', 'ok', 'declined', 'oauth', 'state', 'bot', 'scope', 'link', 'retry', 'banned'])],
  'onboarding.preset': [one(['start-new', 'integrate', 'quiet', 'import', 'songs'])],
  'module.toggles': [shape(NAME), one(['on', 'off'])],
  'custom.commands': [one(['created', 'deleted'])],
  'dashboard.views': [one([
    'overview', 'commands', 'flows', 'automations', 'modules', 'timers', 'loyalty', 'counters',
    'quotes', 'songqueue', 'channelpoints', 'govee', 'billing', 'settings', 'access'
  ]), one(['owner', 'delegate'])],
  'billing.checkout': [one(['monthly', 'single', 'gift']), one(['redirected', 'blocked', 'unavailable', 'rejected'])],
  'import.runs': [one(['streamelements', 'fossabot', 'moobot', 'nightbot', 'streamlabs_desktop', 'wizebot']),
    one(['preview_ok', 'preview_failed', 'committed', 'commit_failed'])],
  'onboarding.steps': [one(['viewed', 'consent_missing', 'save_failed'])],
  'signup.via': [one(VIA), one(['started', 'signup'])]
};

let clientOverride: Client | null | undefined;
type Slot = { key: string; expireAt: number; field: string; n: number };
let pending = new Map<string, Slot>();
let timer: ReturnType<typeof setInterval> | null = null;

function valid(metric: string, dims: string[]): boolean {
  const checks = CATALOG[metric];
  return !!checks && checks.length === dims.length && checks.every((ok, i) => ok(dims[i]));
}

export function count(metric: string, ...dims: string[]): void {
  if (!valid(metric, dims)) return;
  const field = [metric, ...dims].join('|');
  const day = dayKey(new Date());
  const ref = `${day.key}|${field}`;
  const slot = pending.get(ref) ?? { ...day, field, n: 0 };
  slot.n++;
  pending.set(ref, slot);
  timer ??= startTimer();
}

function startTimer(): ReturnType<typeof setInterval> {
  const t = setInterval(() => void flushAnalytics(), FLUSH_MS);
  (t as { unref?: () => void }).unref?.();
  return t;
}

function dayKey(now: Date): { key: string; expireAt: number } {
  const day = now.toISOString().slice(0, 10).replaceAll('-', '');
  const end = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1) / 1000;
  return { key: `an:c:${day}`, expireAt: end + RETAIN_SECONDS };
}

export async function flushAnalytics(): Promise<void> {
  if (pending.size === 0) return;
  const client = clientOverride === undefined ? (masterClient() as Client | null) : clientOverride;
  const batch = pending;
  pending = new Map();
  if (!client) return;
  const pipe = client.pipeline();
  const expiries = new Map<string, number>();
  for (const { key, expireAt, field, n } of batch.values()) {
    pipe.hincrby(key, field, n);
    expiries.set(key, expireAt);
  }
  for (const [key, expireAt] of expiries) pipe.expireat(key, expireAt);
  await pipe.exec().catch(() => undefined);
}

export function setAnalyticsClientForTests(client: Client | null | undefined): void {
  clientOverride = client;
  pending = new Map();
  if (timer) clearInterval(timer);
  timer = null;
}
