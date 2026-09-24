// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CappedBodyError, readCappedText, reasonOf } from '../http';

export const DEFAULT_API_BASE = 'https://fossabot.com';

export const FETCH_TIMEOUT_MS = 10_000;

const MAX_RESPONSE_BODY_BYTES = 16 << 20;

const SLUG_SHAPE = /^[a-z0-9_.-]{1,64}$/;

export class FossabotFetchError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'FossabotFetchError';
  }
}

export interface FetchOptions {
  baseUrl?: string;
  timeoutMs?: number;
}

export interface FbFetchEnvelope {
  channel: unknown;
  roles: unknown;
  commands: unknown;
}

interface Lookup {
  base: string;
  slug: string;
  timeoutMs: number;
}

interface Route {
  lookup: Lookup;
  path: string;
  notFound: string;
}

type Doc = Record<string, unknown>;

const q = (s: string): string => JSON.stringify(s);

export async function fetchFossabot(slug: string, opts: FetchOptions = {}): Promise<FbFetchEnvelope> {
  const lookup = openLookup(slug, opts);
  const channel = channelRecord(await fbGet<Doc>(bySlugRoute(lookup)));
  const doc = await fbGet<Doc>(commandsRoute(lookup, channel));
  return { channel, roles: doc?.roles ?? [], commands: doc?.commands ?? [] };
}

function openLookup(slug: string, opts: FetchOptions): Lookup {
  const wanted = slug.trim().toLowerCase();
  if (wanted === '') throw new FossabotFetchError('fossabot: a channel name is required');
  if (!SLUG_SHAPE.test(wanted))
    throw new FossabotFetchError(
      `fossabot: ${q(slug)} is not a channel name (letters, digits, dot, dash and underscore only)`
    );
  return { base: opts.baseUrl || DEFAULT_API_BASE, slug: wanted, timeoutMs: opts.timeoutMs ?? FETCH_TIMEOUT_MS };
}

function bySlugRoute(lookup: Lookup): Route {
  return {
    lookup,
    path: `/api/v2/cached/channels/by-slug/${encodeURIComponent(lookup.slug)}`,
    notFound: `no Fossabot channel named ${q(lookup.slug)}`
  };
}

function commandsRoute(lookup: Lookup, channel: Doc): Route {
  return {
    lookup,
    path: `/api/v2/cached/channels/${encodeURIComponent(channelId(channel, lookup))}/commands`,
    notFound: `${q(lookup.slug)} publishes no commands directory`
  };
}

function channelId(channel: Doc, lookup: Lookup): string {
  const id = channel.id;
  if (typeof id === 'string' && id.trim() !== '') return id.trim();
  if (typeof id === 'number' && Number.isFinite(id)) return String(id);
  throw new FossabotFetchError(`fossabot: the lookup for ${q(lookup.slug)} answered without a channel id`);
}

function channelRecord(doc: Doc): Doc {
  const v = doc?.channel;
  return v !== null && typeof v === 'object' && !Array.isArray(v) ? (v as Doc) : {};
}

async function fbGet<T>(route: Route): Promise<T> {
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), route.lookup.timeoutMs);
  try {
    return await requestJSON<T>(route, abort.signal);
  } catch (err) {
    if (err instanceof FossabotFetchError) throw err;
    throw new FossabotFetchError(`fossabot: ${route.path}: ${reasonOf(err)}`);
  } finally {
    clearTimeout(timer);
  }
}

async function requestJSON<T>(route: Route, signal: AbortSignal): Promise<T> {
  const res = await fetch(route.lookup.base + route.path, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    signal
  });
  const text = await readBody(res, route);
  if (res.status === 200) return decodeJSON<T>(text, route);
  if (res.status < 500) throw new FossabotFetchError(`fossabot: ${route.notFound}`);
  throw new FossabotFetchError(`fossabot: ${route.path} returned ${res.status}: ${snippet(text)}`);
}

function decodeJSON<T>(text: string, route: Route): T {
  try {
    return JSON.parse(text) as T;
  } catch (err) {
    throw new FossabotFetchError(`fossabot: ${route.path}: decoding response: ${(err as Error).message}`);
  }
}

async function readBody(res: Response, route: Route): Promise<string> {
  try {
    return await readCappedText(res, MAX_RESPONSE_BODY_BYTES);
  } catch (err) {
    const what = err instanceof CappedBodyError ? err.message : reasonOf(err);
    throw new FossabotFetchError(`fossabot: ${route.path}: reading response: ${what}`);
  }
}

const MAX_BODY_SNIPPET = 256;
function snippet(body: string): string {
  let s = body;
  if (s.length > MAX_BODY_SNIPPET) s = s.slice(0, MAX_BODY_SNIPPET) + '…';
  return s.replaceAll('\n', ' ').split(/\s+/).filter(Boolean).join(' ');
}
