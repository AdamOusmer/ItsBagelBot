// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Must match internal/domain/analytics: key layout and the values its catalog accepts.
import { masterClient } from './valkey-master';

export type AnalyticsApp = 'dashboard' | 'admin';
export type ActionOutcome = 'ok' | 'invalid' | 'failed' | 'denied';

type Pipeline = {
  hincrby(key: string, field: string, n: number): Pipeline;
  expireat(key: string, at: number): Pipeline;
  exec(): Promise<unknown>;
};
type Client = { pipeline(): Pipeline };

const FLUSH_MS = 10_000;
const MAX_SLOTS = 2048;
const RETAIN_SECONDS = 7 * 24 * 60 * 60;

const ROUTE = /^\/[A-Za-z0-9_()[\]./=-]{0,110}$/;
const ACTION = /^\/[A-Za-z0-9_()[\]./=-]{0,90}\?\/[A-Za-z0-9_]{1,32}$/;
const NAME = /^[a-z0-9][a-z0-9_.-]{0,63}$/;

const one = (values: readonly string[]) => (v: string) => values.includes(v);
const shape = (re: RegExp) => (v: string) => re.test(v);

const CATALOG: Record<string, ((v: string) => boolean)[]> = {
  'web.views': [one(['dashboard', 'admin']), shape(ROUTE)],
  'web.actions': [one(['dashboard', 'admin']), shape(ACTION), one(['ok', 'invalid', 'failed', 'denied'])],
  'onboarding.preset': [one(['start-new', 'integrate', 'quiet', 'import', 'songs'])],
  'module.toggles': [shape(NAME), one(['on', 'off'])],
  'custom.commands': [one(['created', 'deleted'])]
};

let app: AnalyticsApp | null = null;
let clientOverride: Client | null | undefined;
let pending = new Map<string, number>();
let timer: ReturnType<typeof setInterval> | null = null;

export function setAnalyticsApp(name: AnalyticsApp): void {
  app = name;
}

function valid(metric: string, dims: string[]): boolean {
  const checks = CATALOG[metric];
  return !!checks && checks.length === dims.length && checks.every((ok, i) => ok(dims[i]));
}

export function count(metric: string, ...dims: string[]): void {
  if (!valid(metric, dims)) return;
  const field = [metric, ...dims].join('|');
  if (!pending.has(field) && pending.size >= MAX_SLOTS) return;
  pending.set(field, (pending.get(field) ?? 0) + 1);
  timer ??= startTimer();
}

function startTimer(): ReturnType<typeof setInterval> {
  const t = setInterval(() => void flushAnalytics(), FLUSH_MS);
  (t as { unref?: () => void }).unref?.();
  return t;
}

type ViewEvent = { request: Request; route: { id: string | null }; isDataRequest?: boolean };

function pageRoute(event: ViewEvent): string | null {
  const route = event.route.id;
  if (event.request.method !== 'GET' || !route) return null;
  const html = event.isDataRequest || (event.request.headers.get('accept') ?? '').includes('text/html');
  return html ? route : null;
}

export function countView(event: ViewEvent): void {
  const route = pageRoute(event);
  if (app && route) count('web.views', app, route);
}

type ActionEvent = { route?: { id: string | null }; url?: URL };

function actionField(event: ActionEvent): string | null {
  const route = event.route?.id;
  if (!route || !event.url) return null;
  const search = event.url.search;
  const name = search.startsWith('?/') ? search.slice(2).split('&')[0] : 'default';
  return `${route}?/${name}`;
}

export function countAction(event: ActionEvent, outcome: ActionOutcome): void {
  const field = actionField(event);
  if (app && field) count('web.actions', app, field, outcome);
}

function dayKey(now: Date): { key: string; expireAt: number } {
  const day = now.toISOString().slice(0, 10).replaceAll('-', '');
  const end = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1) / 1000;
  return { key: `an:c:${day}`, expireAt: end + RETAIN_SECONDS };
}

// Loss-tolerant: a failed write drops its counts rather than retrying.
export async function flushAnalytics(now = new Date()): Promise<void> {
  if (pending.size === 0) return;
  const client = clientOverride === undefined ? (masterClient() as Client | null) : clientOverride;
  const batch = pending;
  pending = new Map();
  if (!client) return;
  const { key, expireAt } = dayKey(now);
  const pipe = client.pipeline();
  for (const [field, n] of batch) pipe.hincrby(key, field, n);
  pipe.expireat(key, expireAt);
  await pipe.exec().catch(() => undefined);
}

export function setAnalyticsClientForTests(client: Client | null | undefined): void {
  clientOverride = client;
  pending = new Map();
  app = null;
  if (timer) clearInterval(timer);
  timer = null;
}
