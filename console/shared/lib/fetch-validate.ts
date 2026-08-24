// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// $(urlfetch) definition validation, shared by the dashboard editor (instant
// feedback) and its server actions (authoritative re-check) over the same
// Error strings the commands service mints. Split from commands-validate.ts,
// whose command-shaped validators it grew beside; the token/slug grammar
// lives in fetch-tokens.ts and is re-exported here.
export * from './fetch-tokens';

/** Stored bare/lower-case like a command trigger. Matches Go FetchDefName
 * `^[a-z0-9_]{1,32}$` — one grammar for `{urlfetch:name}` payload heads. */
export const FETCH_NAME_MAX = 32;
/** https only; long enough for signed query strings (512 measured across the
 * APIs broadcasters actually wire up), rejected beyond. */
export const FETCH_URL_MAX = 512;
/** Depth of the dotted JSON path. Mirrors the Go resolver's cap; deeper paths
 * are almost always a sign the author picked the wrong leaf. */
export const JSON_PATH_MAX_DEPTH = 8;
/** Distinct `{urlfetch:…}` references allowed in one command response. Each
 * distinct name fans out to gossip before the line renders, so the cap bounds
 * the chat-latency worst case (3 × ~2.5s in parallel, never serial). */
export const URLFETCH_TOKEN_CAP = 3;
/** Names the sealed key a def uses; display-only label, not the secret. */
export const KEY_LABEL_MAX = 32;
/** Sealed key material ceiling; the value crosses the wire exactly once. */
export const KEY_VALUE_MAX = 512;
/** Definitions per broadcaster. Chat expansion resolves every token in a
 * response, so 20 defs bounds one broadcaster's share of the fan-out budget;
 * enforced synchronously at save by the commands service (COUNT before
 * insert), mirrored here only as an early, friendly stop. */
export const DEFS_PER_BROADCASTER = 20;

const FETCH_NAME_RE = /^[a-z0-9_]+$/;
// Path segments and array indices: indices `\d+` are a subset of the name
// charset, so one regex covers both (mirrors the Go resolver's two rules).
export const PATH_SEGMENT_RE = /^[A-Za-z0-9_-]+$/;

export type FetchKind = 'plain' | 'json';

export interface FetchDefFields {
  /** Normalized slug (slugifyName output). */
  name: string;
  url: string;
  kind: FetchKind;
  /** Path segments; [] for plain defs and for root-scalar json picks. */
  path: string[];
  /** '' = no auth. Must name a stored key at fetch time (dangling fails closed). */
  keyLabel: string;
}

/** field -> human message; empty object = valid. Keys match form field names. */
export type FetchDefErrors = Partial<Record<'name' | 'url' | 'kind' | 'path' | 'key_label', string>>;

function hostIsDenied(host: string): boolean {
  const bare = host.replace(/^\[/, '').replace(/\]$/, '');
  return deniedName(bare) || ipLiteral(bare);
}

// localhost plus the mDNS/intranet-style suffixes; ':' covers every IPv6
// literal form, the regex the dotted-quad IPv4 one.
const DENIED_HOSTS = new Set(['localhost']);
const DENIED_SUFFIXES = ['.local', '.internal'];
const IPV4_LITERAL_RE = /^(\d{1,3}\.){3}\d{1,3}$/;

function deniedName(bare: string): boolean {
  if (DENIED_HOSTS.has(bare)) return true;
  return DENIED_SUFFIXES.some((suffix) => bare.endsWith(suffix));
}

function ipLiteral(bare: string): boolean {
  if (bare.includes(':')) return true;
  return IPV4_LITERAL_RE.test(bare);
}

// Each field's rules live in their own problem function returning the first
// violation (or undefined), so the assembler below spends exactly one branch
// per field — the gate shape this file's validators share with validateCommand.
function fetchNameProblem(f: FetchDefFields): string | undefined {
  if (!f.name) return 'Definition name is required.';
  if (f.name.length > FETCH_NAME_MAX) return `Definition name must be at most ${FETCH_NAME_MAX} characters.`;
  if (!FETCH_NAME_RE.test(f.name)) return 'Use lower-case letters, digits and underscores only.';
  return undefined;
}

function parsedHttpsUrl(url: string): URL | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  if (parsed.protocol !== 'https:') return null;
  if (!parsed.hostname) return null;
  return parsed;
}

function fetchUrlProblem(f: FetchDefFields): string | undefined {
  if (!f.url) return 'URL is required.';
  if (f.url.length > FETCH_URL_MAX) return `URL must be at most ${FETCH_URL_MAX} characters.`;
  const parsed = parsedHttpsUrl(f.url);
  if (!parsed) return 'URL must start with https://';
  if (hostIsDenied(parsed.hostname)) return 'URL must point at a public https host.';
  return undefined;
}

// kind and path validate together (which field errs depends on the kind), so
// the problem carries its own field name.
function kindPathProblem(f: FetchDefFields): { field: 'kind' | 'path'; msg: string } | undefined {
  if (f.kind !== 'plain' && f.kind !== 'json') return { field: 'kind', msg: 'Pick plain or json.' };
  if (f.kind === 'plain') {
    if (f.path.length === 0) return undefined;
    return { field: 'path', msg: 'A plain fetch reads the whole body — clear the path or switch to json.' };
  }
  return jsonPathProblem(f);
}

function jsonPathProblem(f: FetchDefFields): { field: 'path'; msg: string } | undefined {
  if (f.path.length > JSON_PATH_MAX_DEPTH) {
    return { field: 'path', msg: `Path can be at most ${JSON_PATH_MAX_DEPTH} segments deep.` };
  }
  const bad = f.path.find((s) => !PATH_SEGMENT_RE.test(s));
  if (bad === undefined) return undefined;
  return { field: 'path', msg: `"${bad}" cannot be used as a path segment — letters, digits, "-" and "_" only.` };
}

export function validateFetchDef(f: FetchDefFields): FetchDefErrors {
  const errors: FetchDefErrors = {};
  const name = fetchNameProblem(f);
  if (name !== undefined) errors.name = name;
  const url = fetchUrlProblem(f);
  if (url !== undefined) errors.url = url;
  const kindPath = kindPathProblem(f);
  if (kindPath !== undefined) errors[kindPath.field] = kindPath.msg;
  if (f.keyLabel.length > KEY_LABEL_MAX) errors.key_label = `Key label must be at most ${KEY_LABEL_MAX} characters.`;
  return errors;
}

