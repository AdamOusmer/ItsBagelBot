// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Fetch layer of the Fossabot config-import source: turns a public channel
// name into the same stapled envelope ./envelope decodes.
//
// Decision record: why a public directory read and not an API token.
// Fossabot publishes no export button, no OAuth application and no
// authenticated REST API a broadcaster could grant us (verified 2026-09-07).
// What it does publish is every channel's commands directory
// (fossabot.com/<login>/commands), served by an unauthenticated cached API:
//
//	GET /api/v2/cached/channels/by-slug/<slug>   -> {"channel":{"id","login",…}}
//	GET /api/v2/cached/channels/<id>/commands    -> {"roles":[…],"commands":[…]}
//
// So the input is a channel name, not a secret, and the import can only ever
// reach what any viewer of that page can already read: commands hidden from
// the directory, timers, keywords and cooldowns are not in this feed at all
// (/timers and /keywords answer 404), which the instructions step states.
//
// Two calls, because the commands endpoint is keyed by Fossabot's numeric
// channel id and only the by-slug lookup knows it. The lookup doubles as the
// existence check: an unknown name never reaches the second call.

import { CappedBodyError, readCappedText, reasonOf } from '../http';

// DEFAULT_API_BASE is Fossabot's production root. Injectable so tests point
// fetchFossabot at a local server, mirroring the other fetch layers.
export const DEFAULT_API_BASE = 'https://fossabot.com';

// FETCH_TIMEOUT_MS bounds each upstream call via AbortController. Two
// sequential calls happen per fetch, so worst case is ~20s.
export const FETCH_TIMEOUT_MS = 10_000;

// MAX_RESPONSE_BODY caps how much of one upstream reply is read into memory,
// same posture as the Nightbot and StreamElements fetch layers: orders of
// magnitude past the largest directory observed (~120 commands, ~40 KB) while
// bounding a hostile or broken server response.
const MAX_RESPONSE_BODY = 16 << 20;

// SLUG_SHAPE is the gate a channel name must pass before it is allowed near
// the transport, applied after trim().toLowerCase(). Fossabot slugs are Twitch
// logins (and the odd dotted/hyphenated alias), so this is deliberately the
// narrow set: no slash can smuggle a different path onto the constant URL
// template below, no whitespace or control byte can reach a header, and a
// typo is refused before a request is made.
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

// FbFetchEnvelope is the stapled shape ./envelope's decodeEnvelope accepts:
// the channel record from the lookup plus the two collections the commands
// endpoint answers with, carried whole so the parse layer sees exactly what
// the upstream said.
export interface FbFetchEnvelope {
  channel: unknown;
  roles: unknown;
  commands: unknown;
}

// Lookup binds the transport to the one channel being read. Every step below
// takes it whole rather than (base, slug, timeout) triples: the slug is only
// ever used for error prose, and threading it as its own string argument
// through five functions is how a path and a name get swapped without the
// compiler noticing.
interface Lookup {
  base: string;
  slug: string;
  timeoutMs: number;
}

// Route is one resolved GET on a lookup. The path is built from a constant
// template at exactly two call sites; notFound is the prose a 4xx turns into,
// decided where the route is built because only that site knows whether a
// miss means "no such channel" or "no directory for a channel that exists".
interface Route {
  lookup: Lookup;
  path: string;
  notFound: string;
}

type Doc = Record<string, unknown>;

const q = (s: string): string => JSON.stringify(s);

// fetchFossabot reads one channel's public commands directory.
export async function fetchFossabot(slug: string, opts: FetchOptions = {}): Promise<FbFetchEnvelope> {
  const lookup = openLookup(slug, opts);
  const channel = channelRecord(await fbGet<Doc>(bySlugRoute(lookup)));
  const doc = await fbGet<Doc>(commandsRoute(lookup, channel));
  return { channel, roles: doc?.roles ?? [], commands: doc?.commands ?? [] };
}

// openLookup applies the slug gate and binds the transport options.
function openLookup(slug: string, opts: FetchOptions): Lookup {
  const wanted = slug.trim().toLowerCase();
  if (wanted === '') throw new FossabotFetchError('fossabot: a channel name is required');
  if (!SLUG_SHAPE.test(wanted))
    throw new FossabotFetchError(
      `fossabot: ${q(slug)} is not a channel name (letters, digits, dot, dash and underscore only)`
    );
  return { base: opts.baseUrl || DEFAULT_API_BASE, slug: wanted, timeoutMs: opts.timeoutMs ?? FETCH_TIMEOUT_MS };
}

// bySlugRoute resolves a slug to Fossabot's channel record. A 4xx here is the
// normal answer for a name nobody registered, so it reads as the prose a
// broadcaster can act on rather than as a status code; a 5xx is Fossabot
// being down, which is a different problem and says so.
function bySlugRoute(lookup: Lookup): Route {
  return {
    lookup,
    path: `/api/v2/cached/channels/by-slug/${encodeURIComponent(lookup.slug)}`,
    notFound: `no Fossabot channel named ${q(lookup.slug)}`
  };
}

// commandsRoute addresses the directory itself. A 4xx at this point means the
// channel resolved a moment ago but publishes no directory (deleted between
// the two calls, or a shape Fossabot stopped serving), which is worth saying
// plainly instead of surfacing a status code. The id came from Fossabot's own
// reply, never from user text, and is encoded anyway.
function commandsRoute(lookup: Lookup, channel: Doc): Route {
  return {
    lookup,
    path: `/api/v2/cached/channels/${encodeURIComponent(channelId(channel, lookup))}/commands`,
    notFound: `${q(lookup.slug)} publishes no commands directory`
  };
}

// channelId reads the numeric id the commands endpoint is keyed by. Fossabot
// sends it as a string ("30602"); a number is accepted too rather than betting
// the import on a JSON type that costs nothing to tolerate.
function channelId(channel: Doc, lookup: Lookup): string {
  const id = channel.id;
  if (typeof id === 'string' && id.trim() !== '') return id.trim();
  if (typeof id === 'number' && Number.isFinite(id)) return String(id);
  throw new FossabotFetchError(`fossabot: the lookup for ${q(lookup.slug)} answered without a channel id`);
}

// channelRecord unwraps the lookup reply's channel object, tolerating a reply
// that carries none (the id check above then produces the readable error).
function channelRecord(doc: Doc): Doc {
  const v = doc?.channel;
  return v !== null && typeof v === 'object' && !Array.isArray(v) ? (v as Doc) : {};
}

// fbGet performs one routed GET under the lookup's timeout. Anything the
// transport throws is wrapped with the path so the wizard's error names the
// endpoint; prose this layer already shaped passes through untouched.
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

// readBody reads through the shared capped reader; an oversized body is named
// as such because "reading response" alone would read like a network blip.
async function readBody(res: Response, route: Route): Promise<string> {
  try {
    return await readCappedText(res, MAX_RESPONSE_BODY);
  } catch (err) {
    const what = err instanceof CappedBodyError ? err.message : reasonOf(err);
    throw new FossabotFetchError(`fossabot: ${route.path}: reading response: ${what}`);
  }
}

// snippet collapses an upstream error body into one short single-line fragment.
const MAX_BODY_SNIPPET = 256;
function snippet(body: string): string {
  let s = body;
  if (s.length > MAX_BODY_SNIPPET) s = s.slice(0, MAX_BODY_SNIPPET) + '…';
  return s.replaceAll('\n', ' ').split(/\s+/).filter(Boolean).join(' ');
}
